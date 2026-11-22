package download

import (
	"context"
	"errors"
	"fmt"
	"go-downloader/internal/diagnostics"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

var errSegmentUnsafe = errors.New("分片响应的范围、版本或来源不一致，已回退单连接")

func (m *Manager) probeSegments(ctx context.Context, t *task) (partMeta, string, error) {
	source := m.requestSource(t)
	r := byteRange{0, 0}
	resp, reader, closeResponse, err := m.openRange(ctx, t, source, r, "")
	if err != nil {
		return partMeta{}, "", err
	}
	defer closeResponse()
	if resp.StatusCode >= 400 {
		return partMeta{}, "", &httpStatusError{resp.StatusCode, fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))}
	}
	_, _, total, ok := contentRangeBounds(resp.Header.Get("Content-Range"))
	resource := source.url
	if resp.Request != nil {
		u := *resp.Request.URL
		u.User = nil
		resource = u.String()
	}
	etag := resp.Header.Get("ETag")
	if !ok || !exactRange(resp, r, total, etag, resource) {
		return partMeta{}, "服务器未提供可靠的 206、强 ETag 和未压缩表示", nil
	}
	var body [2]byte
	n, readErr := io.ReadFull(reader, body[:])
	if n != 1 || (readErr != io.EOF && readErr != io.ErrUnexpectedEOF) {
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return partMeta{}, "", readErr
		}
		return partMeta{}, "探测响应长度不正确", nil
	}
	if total < segmentThreshold {
		return partMeta{}, "文件小于 8 MiB，使用单连接", nil
	}
	cur := m.current(t)
	if cur.ID != "" && ctx.Err() == nil && fileSize(cur.Path+".part") == 0 && !fileExists(cur.Path+".part.segments") {
		m.adoptName(t, resp)
	}
	return partMeta{SchemaVersion: 2, Mode: "segments", URL: source.url, ResourceURL: resource, ETag: etag, Total: total, Resumable: true}, "", nil
}

// transfer selects once per execution. Unsafe parallel responses are joined
// and discarded before one fallback to the existing sequential retry path.
func (m *Manager) transfer(ctx context.Context, t *task) error {
	cur := m.current(t)
	if cur.ID == "" {
		return errRemoved
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cur.NeedsHeaders {
		return fmt.Errorf("此任务需要请求头，请通过「高级新建」补填")
	}
	if cur.SourceRevision > 0 {
		if err := m.applySourceReset(t); err != nil {
			return err
		}
		cur = m.current(t)
	}
	meta := readPartMeta(cur.Path + ".part.meta")
	hasSegments := fileExists(cur.Path+".part.segments") || meta.SchemaVersion != 0 || meta.Mode != ""
	resume := validSegments(meta, cur.URL) && segmentFileValid(cur.Path+".part.segments", meta)
	if hasSegments && !resume {
		if err := m.discardSegments(t, "分片元信息缺失、损坏或版本未知，已从零恢复"); err != nil {
			return err
		}
		return m.downloadSingle(ctx, t)
	}
	if !resume && (cur.ConnectionLimit <= 1 || fileSize(cur.Path+".part") > 0) {
		m.mu.Lock()
		t.item.TransferMode = "single"
		if cur.ConnectionLimit > 1 {
			t.item.FallbackReason = "保留已有连续部分文件，使用单连接续传"
		} else {
			t.item.FallbackReason = ""
		}
		m.mu.Unlock()
		return m.downloadSingle(ctx, t)
	}
	probe, reason, err := m.probeSegments(ctx, t)
	if err != nil {
		return err
	}
	if reason != "" || (resume && (probe.ETag != meta.ETag || probe.Total != meta.Total || !sameResourceURLs(meta.ResourceURL, probe.ResourceURL))) {
		if reason == "" {
			reason = "资源版本或来源变化，已从零恢复"
		}
		if err := m.discardSegments(t, reason); err != nil {
			return err
		}
		return m.downloadSingle(ctx, t)
	}
	if !resume {
		meta = probe
	}
	err = m.fetchSegments(ctx, t, meta, resume)
	if errors.Is(err, errSegmentUnsafe) && ctx.Err() == nil {
		if err := m.discardSegments(t, errSegmentUnsafe.Error()); err != nil {
			return err
		}
		return m.downloadSingle(ctx, t)
	}
	return err
}

func (m *Manager) fetchSegments(ctx context.Context, t *task, meta partMeta, resume bool) error {
	cur := m.current(t)
	if cur.ID == "" {
		return errRemoved
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := cur.Path + ".part.segments"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("无法打开分片文件：%w", err)
	}
	defer f.Close()
	if !resume {
		if err := f.Truncate(meta.Total); err != nil {
			return fmt.Errorf("无法分配分片文件：%w", err)
		}
		if err := m.syncSegments(f); err != nil {
			return err
		}
		if err := m.writeTaskMeta(t, cur.Path+".part.meta", meta); err != nil {
			return err
		}
	}
	m.beginTransfer(t, meta.Total, completedBytes(meta), meta.ETag, "", true)
	m.mu.Lock()
	t.item.TransferMode = "segments"
	t.item.FallbackReason = ""
	t.segmentCommitted = completedBytes(meta)
	t.segmentProgress = map[int64]int64{}
	for _, r := range meta.Completed {
		t.segmentProgress[r.Start] = r.size()
	}
	m.mu.Unlock()
	// The shared metadata is only mutated under metaMu. Capture immutable
	// response identity separately so worker reads never race with commits.
	identity := meta
	identity.Completed = nil
	completed := map[int64]bool{}
	for _, r := range meta.Completed {
		completed[r.Start] = true
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan byteRange)
	var wg sync.WaitGroup
	var firstErr error
	var once sync.Once
	for range max(1, min(cur.ConnectionLimit, 8)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range jobs {
				if child.Err() != nil {
					return
				}
				err := m.downloadSegment(child, t, f, identity, r)
				if err == nil && child.Err() == nil {
					err = m.commitSegment(t, f, &meta, r)
				}
				if err != nil {
					once.Do(func() { firstErr = err; cancel() })
					return
				}
			}
		}()
	}
dispatch:
	for start := int64(0); start < identity.Total; start += segmentSize {
		if completed[start] {
			continue
		}
		select {
		case jobs <- segmentAt(start, identity.Total):
		case <-child.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil || ctx.Err() != nil {
		m.mu.Lock()
		t.item.Done = t.segmentCommitted
		m.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return firstErr
	}
	if !segmentsCover(meta) || !segmentFileValid(path, meta) {
		return fmt.Errorf("分片未完整覆盖目标文件")
	}
	if err := m.syncSegments(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return m.finalize(t, path, cur.Path+".part.meta", meta.Total, meta.Total)
}

func (m *Manager) downloadSegment(ctx context.Context, t *task, f *os.File, meta partMeta, r byteRange) error {
	for attempt := 1; attempt <= 3; attempt++ {
		err := m.readSegment(ctx, t, f, meta, r)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !retryable(err) || attempt == 3 {
			return err
		}
		m.mu.Lock()
		t.item.RetryCount++
		t.item.Attempt = attempt
		t.item.Error = "分片连接中断，正在重试"
		m.mu.Unlock()
		m.diagnostic(t, diagnostics.Retry, 0, err)
		m.notify()
		timer := time.NewTimer(time.Duration(attempt) * 1500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (m *Manager) readSegment(ctx context.Context, t *task, f *os.File, meta partMeta, r byteRange) error {
	resp, reader, closeResponse, err := m.openRange(ctx, t, m.requestSource(t), r, meta.ETag)
	if err != nil {
		return err
	}
	defer closeResponse()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		return &httpStatusError{resp.StatusCode, fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))}
	}
	if !exactRange(resp, r, meta.Total, meta.ETag, meta.ResourceURL) {
		return errSegmentUnsafe
	}
	buffer := make([]byte, 16*1024)
	var received int64
	for received < r.size() {
		n, readErr := reader.Read(buffer[:min(int64(len(buffer)), r.size()-received)])
		if n > 0 {
			if _, err := f.WriteAt(buffer[:n], r.Start+received); err != nil {
				return fmt.Errorf("写入分片失败：%w", err)
			}
			received += int64(n)
			m.advanceSegment(t, r.Start, received)
		}
		if readErr != nil {
			if readErr == io.EOF && received == r.size() {
				break
			}
			if readErr == io.EOF {
				return io.ErrUnexpectedEOF
			}
			return readErr
		}
	}
	var extra [1]byte
	n, err := reader.Read(extra[:])
	if n > 0 {
		return errSegmentUnsafe
	}
	if err != io.EOF {
		if err == nil {
			return errSegmentUnsafe
		}
		return err
	}
	return nil
}

func (m *Manager) advanceSegment(t *task, start, received int64) {
	m.mu.Lock()
	previous := t.segmentProgress[start]
	if received > previous {
		t.item.Done += received - previous
		t.segmentProgress[start] = received
	}
	m.sampleSpeedLocked(t)
	m.mu.Unlock()
	m.notifyThrottled()
}
