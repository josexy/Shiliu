package proxy

import (
	"bytes"
	"fmt"
	"go-downloader/internal/download"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestSegmentedDownloadsThroughProxiesAndCombinedLimits(t *testing.T) {
	data := bytes.Repeat([]byte("bounded-data"), 700000) // > 8 MiB
	for _, kind := range []Kind{KindHTTP, KindSOCKS5} {
		t.Run(string(kind), func(t *testing.T) {
			var destinationCalls, originCalls, proxyCalls atomic.Int32
			dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				destinationCalls.Add(1)
				for _, key := range []string{"Authorization", "Cookie", "Referer", "X-Secret"} {
					if r.Header.Get(key) != "" {
						t.Errorf("cross-source leaked %s", key)
					}
				}
				if r.Header.Get("Accept-Encoding") != "identity" {
					t.Error("encoding protection missing")
				}
				w.Header().Set("ETag", `"proxy-data"`)
				http.ServeContent(w, r, "file.bin", time.Unix(1700000000, 0), bytes.NewReader(data))
			}))
			defer dest.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originCalls.Add(1)
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("explicit source authentication missing")
				}
				http.Redirect(w, r, dest.URL+"/file", 302)
			}))
			defer origin.Close()
			var address string
			if kind == KindHTTP {
				transport := &http.Transport{}
				defer transport.CloseIdleConnections()
				p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					proxyCalls.Add(1)
					request := r.Clone(r.Context())
					request.RequestURI = ""
					request.Header.Del("Proxy-Authorization")
					resp, err := transport.RoundTrip(request)
					if err != nil {
						http.Error(w, "proxy", 502)
						return
					}
					defer resp.Body.Close()
					for k, values := range resp.Header {
						for _, v := range values {
							w.Header().Add(k, v)
						}
					}
					w.WriteHeader(resp.StatusCode)
					io.Copy(w, resp.Body)
				}))
				defer p.Close()
				address = p.URL
			} else {
				addr, stop := startSOCKS5(t, "user", "pass")
				defer stop()
				address = "http://" + addr
			}
			host, port := splitHostPort(t, address)
			client, err := NewClient(Config{Mode: ModeManual, Kind: kind, Host: host, Port: port, Username: "user", Password: "pass"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			m := download.New(download.Options{Dir: t.TempDir(), AutoStart: true, MaxConcurrent: 2, MaxConnections: 3, RateLimit: 6 * 1024 * 1024, ClientFactory: func() (*http.Client, error) { return client, nil }})
			defer m.Shutdown()
			start := time.Now()
			for i := range 2 {
				_, err = m.AddTask(download.AddOptions{URL: fmt.Sprintf("%s/start/%d", origin.URL, i), ConnectionLimit: 4, TaskRateLimit: 4 * 1024 * 1024, Headers: http.Header{"Authorization": {"Bearer secret"}, "Cookie": {"private=secret"}, "Referer": {"https://private.invalid/"}, "X-Secret": {"secret"}}})
				if err != nil {
					t.Fatal(err)
				}
			}
			for {
				items, summary := m.Snapshot()
				active := 0
				for _, it := range items {
					active += it.ActiveConnections
					if it.ActiveConnections > 4 {
						t.Fatal("per-task connections exceeded")
					}
					if it.State == download.StateFailed {
						t.Fatal(it.Error)
					}
				}
				if active > 3 {
					t.Fatal("global connections exceeded")
				}
				if summary.Completed == 2 {
					for _, it := range items {
						got, _ := os.ReadFile(it.Path)
						if !bytes.Equal(got, data) || it.TransferMode != "segments" {
							t.Fatal("proxy transfer corrupted data")
						}
					}
					break
				}
				if time.Since(start) > 15*time.Second {
					t.Fatal("proxy transfer timed out")
				}
				time.Sleep(5 * time.Millisecond)
			}
			minimum := time.Duration(float64(len(data)*2) / float64(6*1024*1024) * float64(time.Second))
			if time.Since(start) < minimum*9/10 {
				t.Fatal("combined bandwidth ceiling bypassed")
			}
			if originCalls.Load() != 8 || destinationCalls.Load() != 8 || (kind == KindHTTP && proxyCalls.Load() != 16) {
				t.Fatalf("requests origin=%d destination=%d proxy=%d", originCalls.Load(), destinationCalls.Load(), proxyCalls.Load())
			}
		})
	}
}
