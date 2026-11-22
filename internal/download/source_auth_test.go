package download

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestSourceNewAuthenticationAndRedirectIsolation(t *testing.T) {
	var mu sync.Mutex
	var received []http.Header
	end := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.Header.Clone())
		mu.Unlock()
		w.Header().Set("Content-Disposition", `attachment; filename="unwanted.bin"`)
		fmt.Fprint(w, "new body")
	}))
	defer end.Close()
	var first http.Header
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		first = r.Header.Clone()
		mu.Unlock()
		http.Redirect(w, r, end.URL+"/file", http.StatusFound)
	}))
	defer origin.Close()
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	id, err := m.AddTask(AddOptions{URL: "https://old-user:old-password@example.com/file", FileName: "fixed.bin", Headers: http.Header{"Cookie": {"old=old-cookie"}, "X-Old": {"old-custom"}}})
	if err != nil {
		t.Fatal(err)
	}
	url := strings.Replace(origin.URL, "http://", "http://new-user:new-password@", 1) + "/file"
	headers := http.Header{"Cookie": {"new=new-cookie"}, "Referer": {"https://example.com/private"}, "X-New": {"new-custom"}}
	d, err := m.BeginSourceUpdate(id, SourceOptions{URL: url, Headers: headers})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.ProbeSource(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if err = m.CommitSource(d); err != nil {
		t.Fatal(err)
	}
	records := m.Records()
	data, _ := json.Marshal(records)
	meta, _ := os.ReadFile(records[0].Path + ".part.meta")
	for _, secret := range []string{"old-user", "old-password", "old-cookie", "old-custom", "new-user", "new-password", "new-cookie", "new-custom", "example.com/private"} {
		if strings.Contains(string(data)+string(meta), secret) {
			t.Fatalf("persisted secret %s", secret)
		}
	}
	restored := New(Options{Dir: t.TempDir()})
	defer restored.Shutdown()
	restored.Restore(records)
	items, _ := restored.Snapshot()
	if !items[0].NeedsHeaders {
		t.Fatal("restart did not require explicit authentication")
	}
	m.Resume(id)
	item := waitState(t, m, id, StateCompleted)
	if item.Path != records[0].Path {
		t.Fatal("response changed locked path")
	}
	mu.Lock()
	defer mu.Unlock()
	if first.Get("X-Old") != "" || first.Get("Cookie") != "new=new-cookie" || first.Get("X-New") != "new-custom" || first.Get("Authorization") == "" {
		t.Fatal("new source did not exclusively use explicit auth")
	}
	for _, h := range received {
		for _, key := range []string{"Cookie", "Authorization", "Referer", "X-New", "X-Old"} {
			if h.Get(key) != "" {
				t.Fatalf("cross-origin request leaked %s", key)
			}
		}
	}
}

func TestSourceRejectsEngineHeadersAndScrubsErrors(t *testing.T) {
	m := New(Options{Dir: t.TempDir(), ClientFactory: func() (*http.Client, error) {
		return nil, fmt.Errorf("new-pass bearer-secret cookie-secret custom-secret https://new-user:new-pass@example.com/file?token=secret-query")
	}})
	defer m.Shutdown()
	id := addOne(t, m, "https://example.com/old")
	for _, name := range []string{"Range", "If-Range", "Host", "Accept-Encoding", "Content-Length", "Connection", "Proxy-Authorization"} {
		if _, err := m.BeginSourceUpdate(id, SourceOptions{URL: "https://example.com/new", Headers: http.Header{name: {"bad"}}}); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	d, err := m.BeginSourceUpdate(id, SourceOptions{URL: "https://new-user:new-pass@example.com/file", Headers: http.Header{"Authorization": {"Bearer bearer-secret"}, "Cookie": {"key=cookie-secret"}, "X-Custom": {"custom-secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.ProbeSource(context.Background(), d)
	if err == nil {
		t.Fatal("expected probe failure")
	}
	for _, secret := range []string{"new-user", "new-pass", "bearer-secret", "cookie-secret", "custom-secret", "secret-query"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %s", secret)
		}
	}
}
