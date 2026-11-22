package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// A sparse source update validates only durable completed ranges, but EVERY
// byte in each one. Holes and uncommitted attempts are never reused or counted.
func (m *Manager) probeSegmentSource(ctx context.Context, t *task, d *SourceDraft) (SourcePreview, os.FileInfo, partMeta, error) {
	preview := SourcePreview{URL: DiagnosticURL(d.target.display), Total: -1, Reason: "无法证明分片属于同一资源，将从头下载"}
	info, err := os.Stat(d.partPath)
	if err != nil || !info.Mode().IsRegular() {
		return preview, nil, partMeta{}, fmt.Errorf("无法读取分片文件")
	}
	meta := readPartMeta(d.old.Path + ".part.meta")
	eligible := !d.old.SourceReset && validSegments(meta, d.old.URL) && info.Size() == meta.Total && sameResourceURLs(d.old.URL, d.target.display) && len(meta.Completed) > 0
	ranges := []byteRange{{0, 0}}
	if eligible {
		ranges = meta.Completed
	}
	source := requestSource{d.target.url, d.target.user, d.target.pass, d.headers}
	f, err := os.Open(d.partPath)
	if err != nil {
		return preview, info, meta, err
	}
	defer f.Close()
	remote, local := make([]byte, 16*1024), make([]byte, 16*1024)
	for _, r := range ranges {
		etag := ""
		if eligible {
			etag = meta.ETag
		}
		resp, reader, closeResponse, err := m.openRange(ctx, t, source, r, etag)
		if err != nil {
			return preview, info, meta, err
		}
		_, _, total, rangeOK := contentRangeBounds(resp.Header.Get("Content-Range"))
		preview.Total = resp.ContentLength
		if rangeOK {
			preview.Total = total
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			closeResponse()
			return preview, info, meta, &httpStatusError{resp.StatusCode, fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))}
		}
		if !eligible || !exactRange(resp, r, meta.Total, meta.ETag, d.old.URL) {
			var one [1]byte
			_, readErr := reader.Read(one[:])
			closeResponse()
			if readErr != nil && readErr != io.EOF {
				return preview, info, meta, readErr
			}
			return preview, info, meta, nil
		}
		err = func() error {
			defer closeResponse()
			for offset := int64(0); offset < r.size(); {
				n, readErr := reader.Read(remote[:min(int64(len(remote)), r.size()-offset)])
				if n > 0 {
					if _, err := f.ReadAt(local[:n], r.Start+offset); err != nil {
						return err
					}
					if !bytes.Equal(remote[:n], local[:n]) {
						return errSegmentUnsafe
					}
					offset += int64(n)
				}
				if readErr != nil {
					if readErr == io.EOF && offset == r.size() {
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
			if err == io.EOF {
				return nil
			}
			if err == nil {
				return errSegmentUnsafe
			}
			return err
		}()
		if err == errSegmentUnsafe {
			preview.Reason = "已保存分片与新链接不一致，将从头下载"
			return preview, info, meta, nil
		}
		if err != nil {
			return preview, info, meta, err
		}
	}
	after, err := os.Stat(d.partPath)
	if err != nil || !sameVerifiedFile(info, after) {
		return preview, info, meta, fmt.Errorf("分片文件在验证期间发生变化")
	}
	preview.Reuse = true
	preview.Bytes = completedBytes(meta)
	preview.Total = meta.Total
	preview.Reason = "已提交分片已逐字节验证，可保留进度继续下载"
	return preview, info, meta, nil
}
