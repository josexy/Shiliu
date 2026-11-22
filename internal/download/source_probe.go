package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go-downloader/internal/appinfo"
)

type sourceError struct {
	message string
	cause   error
}

func (e *sourceError) Error() string { return e.message }
func (e *sourceError) Unwrap() error { return e.cause }

func sameResource(a, b *url.URL) bool {
	return a != nil && b != nil && sameOrigin(a, b) && a.EscapedPath() == b.EscapedPath()
}

func strongETag(etag string) bool {
	if len(etag) < 2 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		return false
	}
	for i := 1; i < len(etag)-1; i++ {
		if etag[i] == '"' || etag[i] < 0x21 || etag[i] == 0x7f {
			return false
		}
	}
	return true
}

func contentRangeBounds(value string) (start, end, total int64, ok bool) {
	value = strings.TrimSpace(value)
	if len(value) < 6 || !strings.EqualFold(value[:6], "bytes ") {
		return
	}
	span, size, found := strings.Cut(value[6:], "/")
	if !found {
		return
	}
	a, b, found := strings.Cut(span, "-")
	if !found {
		return
	}
	for _, digits := range []string{a, b, size} {
		if digits == "" {
			return
		}
		for _, r := range digits {
			if r < '0' || r > '9' {
				return
			}
		}
	}
	var err error
	if start, err = strconv.ParseInt(a, 10, 64); err != nil {
		return
	}
	if end, err = strconv.ParseInt(b, 10, 64); err != nil {
		return
	}
	if total, err = strconv.ParseInt(size, 10, 64); err != nil {
		return
	}
	ok = start >= 0 && end >= start && total > end
	return
}

// ProbeSource deliberately re-downloads the COMPLETE saved prefix when reuse
// is possible. This costs extra network bytes: equal ETag strings across URLs
// alone are not evidence that their content is identical.
func (m *Manager) ProbeSource(ctx context.Context, d *SourceDraft) (preview SourcePreview, err error) {
	if d == nil || d.manager != m {
		return preview, fmt.Errorf("无效的链接更新草稿")
	}
	d.mu.Lock()
	if d.probing {
		d.mu.Unlock()
		return preview, fmt.Errorf("新链接正在验证")
	}
	d.probing = true
	d.validated = false
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.probing = false; d.mu.Unlock() }()
	m.mu.Lock()
	t, err := m.sourceTaskLocked(d.id)
	if err == nil && (t.sourceGeneration != d.generation || d.ctx.Err() != nil) {
		err = fmt.Errorf("任务已发生变化，请重新打开链接更新")
	}
	if err != nil {
		m.mu.Unlock()
		return preview, err
	}
	m.sourceWG.Add(1)
	t.sourceJobs.Add(1)
	t.sourceWorking++
	m.mu.Unlock()
	defer m.sourceJobDone(t, true)
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(d.ctx, cancel)
	defer stop()
	preview, info, meta, err := m.probeSource(probeCtx, t, d)
	if err != nil {
		temporary := &task{user: d.target.user, pass: d.target.pass, headers: d.headers}
		return SourcePreview{}, &sourceError{message: "验证新链接失败：" + taskError(temporary, err), cause: err}
	}
	m.mu.Lock()
	current := !m.closed && !t.removed && !t.working && t.sourceGeneration == d.generation
	m.mu.Unlock()
	if !current || d.ctx.Err() != nil || probeCtx.Err() != nil {
		return SourcePreview{}, fmt.Errorf("验证已取消或任务发生变化")
	}
	d.mu.Lock()
	d.preview = preview
	d.partInfo = info
	d.meta = meta
	d.validated = true
	d.mu.Unlock()
	return preview, nil
}

func (m *Manager) probeSource(ctx context.Context, t *task, d *SourceDraft) (SourcePreview, os.FileInfo, partMeta, error) {
	if fileExists(d.old.Path + ".part.segments") {
		d.partPath = d.old.Path + ".part.segments"
		return m.probeSegmentSource(ctx, t, d)
	}
	preview := SourcePreview{URL: DiagnosticURL(d.target.display), Total: -1, Reason: "无法证明为同一资源，将从头下载"}
	path := d.old.Path + ".part"
	d.partPath = path
	info, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return preview, nil, partMeta{}, fmt.Errorf("无法读取已有部分文件")
	}
	if errors.Is(err, os.ErrNotExist) {
		info = nil
	}
	if info != nil && !info.Mode().IsRegular() {
		return preview, nil, partMeta{}, fmt.Errorf("部分文件路径不是普通文件")
	}
	var size int64
	if info != nil {
		size = info.Size()
	}
	meta := readPartMeta(path + ".meta")
	oldURL, _ := url.Parse(d.old.URL)
	newURL, _ := url.Parse(d.target.url)
	eligible := size > 0 && !d.old.SourceReset && sameResource(oldURL, newURL) && meta.URL == d.old.URL && strongETag(meta.ETag) && meta.Total >= size && (d.old.Total <= 0 || d.old.Total == meta.Total)
	var local *os.File
	if eligible {
		local, err = os.Open(path)
		if err != nil {
			return preview, info, meta, fmt.Errorf("无法读取已有部分文件")
		}
		defer local.Close()
	}
	client, err := m.opts.ClientFactory()
	if err != nil {
		return preview, info, meta, err
	}
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, d.target.url, nil)
	if err != nil {
		return preview, info, meta, err
	}
	req.Header = d.headers.Clone()
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", appinfo.UserAgent(m.opts.AppVersion))
	}
	req.Header.Set("Accept-Encoding", "identity")
	end := int64(0)
	if eligible {
		end = size - 1
		req.Header.Set("If-Range", meta.ETag)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", end))
	if d.target.user != "" && req.Header.Get("Authorization") == "" {
		req.SetBasicAuth(d.target.user, d.target.pass)
	}
	var stalled atomic.Bool
	release, err := m.acquireConnection(reqCtx, t)
	if err != nil {
		return preview, info, meta, err
	}
	defer release()
	timer := time.AfterFunc(m.stall, func() { stalled.Store(true); cancel() })
	defer timer.Stop()
	resp, err := requestClient(client, d.headers).Do(req)
	timer.Stop()
	if err != nil {
		if stalled.Load() {
			err = errStalled
		}
		return preview, info, meta, err
	}
	defer resp.Body.Close()
	if stalled.Load() {
		return preview, info, meta, errStalled
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		if total, ok := parseUnsatisfiedRange(resp.Header.Get("Content-Range")); ok && total == 0 {
			preview.Total = 0
			return preview, info, meta, nil
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return preview, info, meta, &httpStatusError{code: resp.StatusCode, status: fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))}
	}
	preview.Total = resp.ContentLength
	start, last, total, validRange := contentRangeBounds(resp.Header.Get("Content-Range"))
	if resp.StatusCode == http.StatusPartialContent {
		preview.Total = -1
		if validRange {
			preview.Total = total
		}
	}
	reuse := eligible && resp.StatusCode == http.StatusPartialContent && validRange && start == 0 && last == size-1 && total == meta.Total && resp.Header.Get("ETag") == meta.ETag && sameResource(oldURL, resp.Request.URL) && (resp.Header.Get("Content-Encoding") == "" || strings.EqualFold(resp.Header.Get("Content-Encoding"), "identity")) && (resp.ContentLength < 0 || resp.ContentLength == size)
	reader := networkReader{ctx: reqCtx, manager: m, task: t, body: resp.Body, timer: timer, stalled: &stalled}
	if !reuse {
		if resp.ContentLength != 0 {
			var one [1]byte
			n, readErr := reader.Read(one[:])
			if readErr != nil && readErr != io.EOF {
				return preview, info, meta, readErr
			}
			if n == 0 && resp.ContentLength > 0 {
				return preview, info, meta, io.ErrUnexpectedEOF
			}
		}
		if size == 0 {
			preview.Reason = "没有可复用的部分数据，将从头下载"
		}
		return preview, info, meta, nil
	}
	remote, existing := make([]byte, 16*1024), make([]byte, 16*1024)
	var compared int64
	for compared < size {
		n, readErr := reader.Read(remote[:min(int64(len(remote)), size-compared)])
		if n > 0 {
			if _, err := io.ReadFull(local, existing[:n]); err != nil {
				return preview, info, meta, fmt.Errorf("读取已有部分文件失败或文件已变化")
			}
			if !bytes.Equal(remote[:n], existing[:n]) {
				preview.Reason = "已有前缀与新链接内容不同，将从头下载"
				return preview, info, meta, nil
			}
			compared += int64(n)
		}
		if readErr != nil {
			if readErr == io.EOF && compared == size {
				break
			}
			if readErr == io.EOF {
				readErr = io.ErrUnexpectedEOF
			}
			return preview, info, meta, readErr
		}
	}
	var extra [1]byte
	n, readErr := reader.Read(extra[:])
	if n > 0 {
		preview.Reason = "分段响应长度不符，将从头下载"
		return preview, info, meta, nil
	}
	if readErr != io.EOF {
		if readErr != nil {
			return preview, info, meta, readErr
		}
		return preview, info, meta, fmt.Errorf("无法确认分段响应已完整结束")
	}
	after, err := os.Stat(path)
	if err != nil || !sameVerifiedFile(info, after) {
		return preview, info, meta, fmt.Errorf("部分文件在验证期间发生变化")
	}
	preview.Reuse = true
	preview.Bytes = size
	preview.Reason = "已有前缀已逐字节验证，可保留进度继续下载"
	return preview, info, meta, nil
}
