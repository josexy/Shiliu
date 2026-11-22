package download

import (
	"encoding/json"
	"fmt"
	"go-downloader/internal/diagnostics"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type reentrantDiagnosticSink struct {
	manager *Manager
	buffer  diagnostics.Buffer
}

func (s *reentrantDiagnosticSink) Record(e diagnostics.Event) {
	s.manager.Snapshot()
	s.buffer.Record(e)
}
func (s *reentrantDiagnosticSink) Omit(n uint64) { s.manager.Snapshot(); s.buffer.Omit(n) }

func TestDiagnosticHTTPRetryCorrelationAndLockBoundary(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
		}
		fmt.Fprint(w, "echo-body-secret Authorization: bearer-secret Cookie: cookie-secret")
	}))
	defer srv.Close()
	sink := &reentrantDiagnosticSink{}
	m := New(Options{Dir: t.TempDir(), AutoStart: true, Diagnostics: sink})
	sink.manager = m
	defer m.Shutdown()
	id, err := m.AddTask(AddOptions{URL: srv.URL + "/file?signature=query-secret", Headers: http.Header{"Authorization": {"bearer-secret"}, "Cookie": {"cookie-secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, id, StateCompleted)
	m.Shutdown()
	s := sink.buffer.Snapshot()
	seen := map[diagnostics.Kind]bool{}
	status503 := false
	for _, e := range s.Events {
		if e.TaskID != id {
			t.Fatal("unrelated task event", e)
		}
		seen[e.Kind] = true
		if e.HTTPStatus == 503 {
			status503 = true
		}
		if e.Kind == diagnostics.Retry && e.Retry != 1 {
			t.Fatal("retry ordinal", e)
		}
	}
	for _, kind := range []diagnostics.Kind{diagnostics.Queued, diagnostics.Connecting, diagnostics.Downloading, diagnostics.HTTP, diagnostics.Retry, diagnostics.Completed} {
		if !seen[kind] {
			t.Fatal("missing event", kind)
		}
	}
	if !status503 {
		t.Fatal("503 not captured")
	}
	data, _ := json.Marshal(s)
	for _, secret := range []string{"query-secret", "cookie-secret", "bearer-secret", "echo-body-secret", m.dir} {
		if strings.Contains(string(data), secret) {
			t.Fatal("secret in event buffer", secret)
		}
	}
}

func TestDiagnosticUnknownFactoryErrorDoesNotEcho(t *testing.T) {
	log := &diagnostics.Buffer{}
	m := New(Options{Dir: t.TempDir(), AutoStart: true, Diagnostics: log, ClientFactory: func() (*http.Client, error) { return nil, fmt.Errorf("proxy-password plain-secret cGxhaW4tc2VjcmV0") }})
	defer m.Shutdown()
	id := addOne(t, m, "https://example.invalid/file")
	waitState(t, m, id, StateFailed)
	m.Shutdown()
	found := false
	for _, e := range log.Snapshot().Events {
		if e.Kind == diagnostics.ProxyFailure && e.ErrorCode == "unknown" && e.TaskID == id {
			found = true
		}
	}
	data, _ := json.Marshal(log.Snapshot())
	if !found || strings.Contains(string(data), "secret") || strings.Contains(string(data), "cGxhaW4tc2VjcmV0") {
		t.Fatal("unknown error leaked or lost association")
	}
}
