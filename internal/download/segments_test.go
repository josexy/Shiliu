package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func segmentData() []byte {
	b := make([]byte, segmentThreshold+12345)
	for i := range b {
		b[i] = byte(i*31 + i>>16)
	}
	return b
}
func serveSegmentData(w http.ResponseWriter, r *http.Request, data []byte) {
	w.Header().Set("ETag", `"stable"`)
	http.ServeContent(w, r, "payload.bin", time.Unix(1700000000, 0), bytes.NewReader(data))
}

func TestSegmentsDownloadAndIndependentVerification(t *testing.T) {
	data := segmentData()
	var requests, complete atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); serveSegmentData(w, r, data) }))
	defer s.Close()
	for _, matching := range []bool{true, false} {
		t.Run(fmt.Sprint(matching), func(t *testing.T) {
			sum := sha256.Sum256(data)
			expected := hex.EncodeToString(sum[:])
			if !matching {
				expected = strings.Repeat("0", 64)
			}
			m := New(Options{Dir: t.TempDir(), AutoStart: true, MaxConnections: 2, OnComplete: func(Item) { complete.Add(1) }})
			defer m.Shutdown()
			id, err := m.AddTask(AddOptions{URL: s.URL + "/file", ConnectionLimit: 4, ExpectedSHA256: expected})
			if err != nil {
				t.Fatal(err)
			}
			it := waitState(t, m, id, StateCompleted)
			got, _ := os.ReadFile(it.Path)
			if !bytes.Equal(got, data) || it.TransferMode != "segments" {
				t.Fatal("incorrect segmented publication")
			}
			if fileExists(it.Path+".part.segments") || fileExists(it.Path+".part.meta") {
				t.Fatal("temporary files retained")
			}
			want := VerificationMatched
			if !matching {
				want = VerificationMismatched
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				items, _ := m.Snapshot()
				if items[0].VerificationState == want {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("verification did not complete")
				}
				time.Sleep(time.Millisecond * 5)
			}
		})
	}
	if requests.Load() != 8 || complete.Load() != 2 {
		t.Fatalf("requests=%d completions=%d", requests.Load(), complete.Load())
	}
}

func TestSegmentsSelectionAndUnsafeFallback(t *testing.T) {
	data := segmentData()
	for _, mode := range []string{"accept-ranges-only", "weak", "compressed", "unknown", "small", "wrong-probe", "version", "bounds", "full-response", "redirect", "long"} {
		t.Run(mode, func(t *testing.T) {
			var full, segments atomic.Int32
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveSegmentData(w, r, data) }))
			defer other.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rangeHeader := r.Header.Get("Range")
				if rangeHeader == "" {
					full.Add(1)
					w.Write(data)
					return
				}
				if rangeHeader == "bytes=0-0" {
					switch mode {
					case "accept-ranges-only":
						w.Header().Set("Accept-Ranges", "bytes")
						w.Write(data)
						return
					case "weak", "compressed", "unknown", "wrong-probe":
						w.Header().Set("ETag", `"stable"`)
						w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", len(data)))
						if mode == "weak" {
							w.Header().Set("ETag", `W/"stable"`)
						}
						if mode == "compressed" {
							w.Header().Set("Content-Encoding", "gzip")
						}
						if mode == "unknown" {
							w.Header().Set("Content-Range", "bytes 0-0/*")
						}
						if mode == "wrong-probe" {
							w.Header().Set("Content-Range", fmt.Sprintf("bytes 1-1/%d", len(data)))
						}
						w.WriteHeader(206)
						w.Write(data[:1])
						return
					case "small":
						serveSegmentData(w, r, data[:1024])
						return
					}
					serveSegmentData(w, r, data)
					return
				}
				segments.Add(1)
				if mode == "redirect" {
					http.Redirect(w, r, other.URL+"/different", 302)
					return
				}
				if mode == "full-response" {
					w.Write(data)
					return
				}
				var start, end int64
				fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
				w.Header().Set("ETag", `"stable"`)
				if mode == "version" {
					w.Header().Set("ETag", `"changed"`)
				}
				if mode == "bounds" {
					start++
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
				w.WriteHeader(206)
				if mode == "long" {
					w.(http.Flusher).Flush()
				}
				w.Write(data[start : end+1])
				if mode == "long" {
					w.Write([]byte("extra"))
				}
			}))
			defer s.Close()
			m := New(Options{Dir: t.TempDir(), AutoStart: true})
			defer m.Shutdown()
			id, err := m.AddTask(AddOptions{URL: s.URL + "/file", ConnectionLimit: 3})
			if err != nil {
				t.Fatal(err)
			}
			it := waitState(t, m, id, StateCompleted)
			got, _ := os.ReadFile(it.Path)
			if !bytes.Equal(got, data) || it.TransferMode != "single" || it.FallbackReason == "" || full.Load() != 1 {
				t.Fatalf("unsafe fallback: mode=%s full=%d reason=%s", it.TransferMode, full.Load(), it.FallbackReason)
			}
		})
	}
}

func TestSegmentsShortReadRetriesOnlyMissingRange(t *testing.T) {
	data := segmentData()
	var mu sync.Mutex
	counts := map[string]int{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Range")
		mu.Lock()
		counts[h]++
		n := counts[h]
		mu.Unlock()
		if h == fmt.Sprintf("bytes=0-%d", segmentSize-1) && n == 1 {
			w.Header().Set("ETag", `"stable"`)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", segmentSize-1, len(data)))
			w.Header().Set("Content-Length", fmt.Sprint(segmentSize))
			w.WriteHeader(206)
			w.Write(data[:1000])
			return
		}
		serveSegmentData(w, r, data)
	}))
	defer s.Close()
	m := New(Options{Dir: t.TempDir(), AutoStart: true})
	defer m.Shutdown()
	id, err := m.AddTask(AddOptions{URL: s.URL + "/file", ConnectionLimit: 3})
	if err != nil {
		t.Fatal(err)
	}
	it := waitState(t, m, id, StateCompleted)
	got, _ := os.ReadFile(it.Path)
	if !bytes.Equal(got, data) || it.RetryCount != 1 || it.Done != int64(len(data)) || it.NetworkBytes != int64(len(data)+1001) {
		t.Fatalf("retry progress/traffic wrong: %+v", it)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts[fmt.Sprintf("bytes=%d-%d", segmentSize, 2*segmentSize-1)] != 1 {
		t.Fatal("retried a completed range")
	}
}

func TestSegmentsSyncAndCommitFaultsNeverRecoverHoles(t *testing.T) {
	data := segmentData()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveSegmentData(w, r, data) }))
	defer s.Close()
	for _, fault := range []string{"sync", "metadata"} {
		t.Run(fault, func(t *testing.T) {
			m := New(Options{Dir: t.TempDir()})
			id, err := m.AddTask(AddOptions{URL: s.URL + "/file", ConnectionLimit: 3})
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			if fault == "sync" {
				m.syncSegments = func(f *os.File) error {
					if calls.Add(1) > 1 {
						return errors.New("injected Sync failure")
					}
					return f.Sync()
				}
			} else {
				m.metaWrite = func(p string, meta partMeta) error {
					if len(meta.Completed) > 0 {
						return errors.New("injected metadata failure")
					}
					return writePartMeta(p, meta)
				}
			}
			m.Resume(id)
			it := waitState(t, m, id, StateFailed)
			m.Shutdown()
			if fileExists(it.Path) {
				t.Fatal("published after commit fault")
			}
			records := m.Records()
			if records[0].Done != 0 {
				t.Fatal("uncommitted bytes persisted")
			}
			restored := New(Options{Dir: t.TempDir()})
			defer restored.Shutdown()
			restored.Restore(records)
			items, _ := restored.Snapshot()
			if items[0].Done != 0 {
				t.Fatal("sparse length counted as progress")
			}
			restored.Resume(id)
			done := waitState(t, restored, id, StateCompleted)
			got, _ := os.ReadFile(done.Path)
			if !bytes.Equal(got, data) {
				t.Fatal("recovered holes as data")
			}
		})
	}
}

func TestConnectionFIFOAndDynamicLowering(t *testing.T) {
	g := &connectionGate{}
	g.set(2)
	a, _ := g.acquire(context.Background())
	b, _ := g.acquire(context.Background())
	g.set(1)
	first := make(chan func(), 1)
	second := make(chan func(), 1)
	go func() { r, _ := g.acquire(context.Background()); first <- r }()
	for {
		g.mu.Lock()
		n := len(g.queue)
		g.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() { _, err := g.acquire(ctx); canceled <- err }()
	go func() { r, _ := g.acquire(context.Background()); second <- r }()
	cancel()
	if !errors.Is(<-canceled, context.Canceled) {
		t.Fatal("queued cancel failed")
	}
	a()
	select {
	case <-first:
		t.Fatal("granted above lowered cap")
	default:
	}
	b()
	r1 := <-first
	select {
	case <-second:
		t.Fatal("FIFO bypass")
	default:
	}
	r1()
	r2 := <-second
	r2()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active != 0 || len(g.queue) != 0 {
		t.Fatal("leaked permits")
	}
}
