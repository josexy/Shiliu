package download

import (
	"context"
	"fmt"
	"go-downloader/internal/appinfo"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type requestSource struct {
	url, user, pass string
	headers         http.Header
}

func (m *Manager) requestSource(t *task) requestSource {
	m.mu.Lock()
	defer m.mu.Unlock()
	return requestSource{t.item.URL, t.user, t.pass, t.headers.Clone()}
}

// The lease covers headers and the entire response, including redirects. The
// caller always closes it before another request or a retry delay.
func (m *Manager) openRange(ctx context.Context, t *task, source requestSource, r byteRange, etag string) (*http.Response, networkReader, func(), error) {
	reqCtx, cancel := context.WithCancel(ctx)
	release, err := m.acquireConnection(reqCtx, t)
	if err != nil {
		cancel()
		return nil, networkReader{}, nil, err
	}
	cleanup := func() { cancel(); release() }
	client, err := m.opts.ClientFactory()
	if err != nil {
		cleanup()
		return nil, networkReader{}, nil, err
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, source.url, nil)
	if err != nil {
		cleanup()
		return nil, networkReader{}, nil, err
	}
	req.Header = source.headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", appinfo.UserAgent(m.opts.AppVersion))
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.Start, r.End))
	if etag != "" {
		req.Header.Set("If-Range", etag)
	}
	if source.user != "" && req.Header.Get("Authorization") == "" {
		req.SetBasicAuth(source.user, source.pass)
	}
	stalled := &atomic.Bool{}
	timer := time.AfterFunc(m.stall, func() { stalled.Store(true); cancel() })
	resp, err := requestClient(client, source.headers).Do(req)
	timer.Stop()
	if err != nil {
		cleanup()
		if stalled.Load() {
			err = errStalled
		}
		return nil, networkReader{}, nil, err
	}
	closeResponse := func() { timer.Stop(); resp.Body.Close(); cleanup() }
	if stalled.Load() {
		closeResponse()
		return nil, networkReader{}, nil, errStalled
	}
	reader := networkReader{reqCtx, m, t, resp.Body, timer, stalled}
	return resp, reader, closeResponse, nil
}

func exactRange(resp *http.Response, r byteRange, total int64, etag, resource string) bool {
	start, end, size, ok := contentRangeBounds(resp.Header.Get("Content-Range"))
	u, err := url.Parse(resource)
	encoding := resp.Header.Get("Content-Encoding")
	return resp.StatusCode == http.StatusPartialContent && ok && start == r.Start && end == r.End && size == total && resp.Header.Get("ETag") == etag && strongETag(etag) && (encoding == "" || strings.EqualFold(encoding, "identity")) && !resp.Uncompressed && (resp.ContentLength < 0 || resp.ContentLength == r.size()) && err == nil && resp.Request != nil && sameResource(u, resp.Request.URL)
}

func sameResourceURLs(a, b string) bool {
	left, e1 := url.Parse(a)
	right, e2 := url.Parse(b)
	return e1 == nil && e2 == nil && sameResource(left, right)
}
