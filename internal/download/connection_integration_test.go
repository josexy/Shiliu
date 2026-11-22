package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countedTransport struct {
	base         http.RoundTripper
	active, peak atomic.Int32
}
type countedBody struct {
	io.ReadCloser
	once  sync.Once
	count *atomic.Int32
}

func (b *countedBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() { b.count.Add(-1) })
	return err
}
func (tr *countedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	n := tr.active.Add(1)
	for p := tr.peak.Load(); n > p && !tr.peak.CompareAndSwap(p, n); p = tr.peak.Load() {
	}
	resp, err := tr.base.RoundTrip(r)
	if err != nil {
		tr.active.Add(-1)
		return nil, err
	}
	resp.Body = &countedBody{ReadCloser: resp.Body, count: &tr.active}
	return resp, nil
}

func TestConnectionBudgetSharedByProbesSequentialSegmentsAndRetries(t *testing.T) {
	data := segmentData()
	var failed atomic.Bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/multi" && r.Header.Get("Range") == fmt.Sprintf("bytes=0-%d", segmentSize-1) && failed.CompareAndSwap(false, true) {
			w.WriteHeader(503)
			return
		}
		serveSegmentData(w, r, data)
	}))
	defer s.Close()
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	tr := &countedTransport{base: base}
	m := New(Options{Dir: t.TempDir(), MaxConcurrent: 3, MaxConnections: 2, ClientFactory: func() (*http.Client, error) { return &http.Client{Transport: tr}, nil }})
	defer m.Shutdown()
	one, _ := m.AddTask(AddOptions{URL: s.URL + "/single"})
	multi, _ := m.AddTask(AddOptions{URL: s.URL + "/multi", ConnectionLimit: 3})
	probeID, _ := m.AddTask(AddOptions{URL: s.URL + "/old"})
	d, err := m.BeginSourceUpdate(probeID, SourceOptions{URL: s.URL + "/new"})
	if err != nil {
		t.Fatal(err)
	}
	probeDone := make(chan error, 1)
	go func() { _, err := m.ProbeSource(context.Background(), d); probeDone <- err }()
	m.Resume(one)
	m.Resume(multi)
	if err = <-probeDone; err != nil {
		t.Fatal(err)
	}
	waitState(t, m, one, StateCompleted)
	waitState(t, m, multi, StateCompleted)
	m.Shutdown()
	if tr.peak.Load() > 2 || tr.active.Load() != 0 || !failed.Load() {
		t.Fatalf("connections peak=%d retained=%d", tr.peak.Load(), tr.active.Load())
	}
}

func TestSegmentsCancelBudgetRetryAndPendingWrites(t *testing.T) {
	data := segmentData()
	for _, operation := range []string{"pause", "remove", "shutdown", "retry-wait"} {
		t.Run(operation, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if operation == "retry-wait" && r.Header.Get("Range") != "bytes=0-0" {
					w.WriteHeader(503)
					return
				}
				serveSegmentData(w, r, data)
			}))
			defer s.Close()
			m := New(Options{Dir: t.TempDir(), MaxConnections: 1})
			defer m.Shutdown()
			id, err := m.AddTask(AddOptions{URL: s.URL + "/file", ConnectionLimit: 3})
			if err != nil {
				t.Fatal(err)
			}
			m.Resume(id)
			deadline := time.Now().Add(5 * time.Second)
			for {
				it := m.current(m.byID[id])
				if (operation == "retry-wait" && it.RetryCount > 0) || (operation != "retry-wait" && it.TransferMode == "segments") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("did not enter transfer")
				}
				time.Sleep(time.Millisecond)
			}
			m.SetTaskRateLimit(id, 1)
			m.mu.Lock()
			task := m.byID[id]
			done := task.done
			m.mu.Unlock()
			start := time.Now()
			switch operation {
			case "remove":
				m.Remove(id, false)
			case "shutdown":
				m.Shutdown()
			default:
				m.Pause(id)
			}
			if done != nil {
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("cancel did not join workers")
				}
			}
			if time.Since(start) > time.Second {
				t.Fatal("cancellation blocked")
			}
			m.connections.mu.Lock()
			active, waiting := m.connections.active, len(m.connections.queue)
			m.connections.mu.Unlock()
			if active != 0 || waiting != 0 {
				t.Fatal("cancellation leaked leases")
			}
		})
	}
}

func TestSegmentsRetryExhaustion(t *testing.T) {
	data := segmentData()
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == fmt.Sprintf("bytes=%d-%d", segmentSize, 2*segmentSize-1) {
			requests.Add(1)
			w.WriteHeader(503)
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
	it := waitState(t, m, id, StateFailed)
	if requests.Load() != 3 || it.RetryCount != 2 || fileExists(it.Path) {
		t.Fatalf("unbounded retries or premature publish: requests=%d", requests.Load())
	}
	if m.Records()[0].Done != segmentSize+int64(len(data))-2*segmentSize {
		t.Fatal("discarded valid sibling segments")
	}
}
