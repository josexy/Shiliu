package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sourcePartial(t *testing.T, m *Manager, raw string, data []byte, prefix int, etag string) (string, string) {
	t.Helper()
	id, err := m.AddTask(AddOptions{URL: raw, FileName: "fixed.bin"})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := m.Snapshot()
	it, _ := itemByID(items, id)
	if err := os.WriteFile(it.Path+".part", data[:prefix], 0600); err != nil {
		t.Fatal(err)
	}
	if err := writePartMeta(it.Path+".part.meta", partMeta{URL: raw, ETag: etag, Total: int64(len(data)), Resumable: true}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	task := m.byID[id]
	task.item.Total = int64(len(data))
	task.item.Done = int64(prefix)
	task.etag = etag
	m.mu.Unlock()
	return id, it.Path
}

func TestSourceProbeRequiresCompletePrefixProof(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 64*1024)
	const prefix = 40 * 1024
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		body := append([]byte(nil), data...)
		etag := `"same"`
		if mode == "weak" {
			etag = `W/"same"`
		}
		w.Header().Set("ETag", etag)
		switch mode {
		case "different":
			body[prefix-1] = 'b'
		case "full":
			r.Header.Del("Range")
		case "redirect":
			http.Redirect(w, r, "/other?mode=same", http.StatusFound)
			return
		case "bad-range":
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 1-%d/%d", prefix, len(data)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[:prefix])
			return
		}
		http.ServeContent(w, r, "data.bin", time.Unix(1700000000, 0), bytes.NewReader(body))
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	other := httptest.NewServer(handler)
	defer other.Close()
	for _, tc := range []struct {
		name, path, etag string
		reuse            bool
	}{
		{"query", "/file?mode=same", `"same"`, true},
		{"different-tail", "/file?mode=different", `"same"`, false},
		{"weak", "/file?mode=weak", `W/"same"`, false},
		{"no-validator", "/file?mode=same", "", false},
		{"path", "/other?mode=same", `"same"`, false},
		{"origin", other.URL + "/file?mode=same", `"same"`, false},
		{"full", "/file?mode=full", `"same"`, false},
		{"bad-range", "/file?mode=bad-range", `"same"`, false},
		{"redirect", "/file?mode=redirect", `"same"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Options{Dir: t.TempDir()})
			defer m.Shutdown()
			old := server.URL + "/file?mode=old"
			id, path := sourcePartial(t, m, old, data, prefix, tc.etag)
			next := tc.path
			if strings.HasPrefix(next, "/") {
				next = server.URL + next
			}
			d, err := m.BeginSourceUpdate(id, SourceOptions{URL: next})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Cancel()
			preview, err := m.ProbeSource(context.Background(), d)
			if err != nil || preview.Reuse != tc.reuse {
				t.Fatalf("probe: %+v %v", preview, err)
			}
			if preview.Reuse && preview.Bytes != prefix {
				t.Fatal("probe did not verify full prefix")
			}
			if strings.Contains(preview.URL, "?") {
				t.Fatal("preview leaked query")
			}
			got, err := os.ReadFile(path + ".part")
			if err != nil || !bytes.Equal(got, data[:prefix]) {
				t.Fatal("probe changed original data")
			}
			items, _ := m.Snapshot()
			if items[0].URL != old {
				t.Fatal("probe committed URL before confirmation")
			}
		})
	}
}

func TestSourceProbeFailureAndCancelKeepOriginal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	old := server.URL + "/file?old=private"
	id, path := sourcePartial(t, m, old, []byte("old data"), 4, `"same"`)
	d, err := m.BeginSourceUpdate(id, SourceOptions{URL: strings.Replace(server.URL, "http://", "http://user:secret@", 1) + "/file?new=private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ProbeSource(context.Background(), d); err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
		t.Fatalf("failure: %v", err)
	}
	if err := m.CommitSource(d); err == nil {
		t.Fatal("failed probe could commit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.ProbeSource(ctx, d); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	got, _ := os.ReadFile(path + ".part")
	items, _ := m.Snapshot()
	if string(got) != "old " || items[0].URL != old {
		t.Fatal("failed probe damaged original")
	}
}

func TestSourceProbeUsesProxyAndBothRateLimits(t *testing.T) {
	data := bytes.Repeat([]byte("p"), 64)
	var calls atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Host != "example.invalid" || r.Header.Get("If-Range") != `"same"` {
			t.Error("proxy did not receive expected probe")
		}
		w.Header().Set("ETag", `"same"`)
		http.ServeContent(w, r, "data", time.Unix(1700000000, 0), bytes.NewReader(data))
	}))
	defer proxyServer.Close()
	proxyURL, _ := url.Parse(proxyServer.URL)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	m := New(Options{Dir: t.TempDir(), RateLimit: 32, StallTimeout: 30 * time.Millisecond, ClientFactory: func() (*http.Client, error) { return &http.Client{Transport: transport}, nil }})
	defer m.Shutdown()
	id, _ := sourcePartial(t, m, "http://example.invalid/file?old", data, 32, `"same"`)
	if err := m.SetTaskRateLimit(id, 16); err != nil {
		t.Fatal(err)
	}
	d, err := m.BeginSourceUpdate(id, SourceOptions{URL: "http://example.invalid/file?new"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	preview, err := m.ProbeSource(context.Background(), d)
	if err != nil || !preview.Reuse || calls.Load() != 1 || time.Since(start) < time.Second {
		t.Fatalf("throttled proxy probe: %+v %v %v", preview, err, time.Since(start))
	}
	items, _ := m.Snapshot()
	if items[0].RetryCount != 0 {
		t.Fatal("rate wait was treated as stall")
	}
}

func TestSourceProbeCancellationDoesNotHoldManagerLock(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 256)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"same"`)
		http.ServeContent(w, r, "data", time.Unix(1700000000, 0), bytes.NewReader(data))
	}))
	defer server.Close()
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	id, _ := sourcePartial(t, m, server.URL+"/file?old", data, 128, `"same"`)
	if err := m.SetTaskRateLimit(id, 1); err != nil {
		t.Fatal(err)
	}
	d, err := m.BeginSourceUpdate(id, SourceOptions{URL: server.URL + "/file?new"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := m.ProbeSource(ctx, d); finished <- err }()
	time.Sleep(30 * time.Millisecond)
	start := time.Now()
	m.Snapshot()
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("probe held manager lock")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("probe did not cancel quota wait")
	}
}
