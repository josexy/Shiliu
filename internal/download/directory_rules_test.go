package download

import (
	"context"
	"fmt"
	"go-downloader/internal/settings"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryRulesUnifyAdmissionAndOverrides(t *testing.T) {
	base := t.TempDir()
	archive := filepath.Join(base, "archives")
	override := filepath.Join(base, "explicit")
	m := New(Options{Dir: base, DirectoryRules: []settings.DirectoryRule{{ID: "zip", Enabled: true, Kind: "extension", Pattern: ".ZIP", Dir: archive}}})
	defer m.Shutdown()
	addOne(t, m, "https://example.com/quick.zip")
	if _, _, err := m.AddContext(context.Background(), "https://example.com/import.ZIP"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddTask(AddOptions{URL: "https://example.com/advanced", FileName: "chosen.zip", Dir: base}); err != nil {
		t.Fatal(err)
	}
	id, err := m.AddTask(AddOptions{URL: "https://example.com/explicit.zip", Dir: override, DirOverride: true})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := m.Snapshot()
	for _, it := range items {
		want := archive
		if it.ID == id {
			want = override
		}
		if filepath.Dir(it.Path) != want {
			t.Fatal("wrong admission directory", it.Path, want)
		}
	}
	blocked := filepath.Join(base, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := []settings.DirectoryRule{{ID: "bad", Enabled: true, Kind: "extension", Pattern: "bad", Dir: filepath.Join(blocked, "child")}}
	if err := m.SetDirectoryRules(bad); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.AddContext(context.Background(), "https://example.com/no.bad"); err == nil {
		t.Fatal("silently fell back to default")
	}
	if _, err := m.AddTask(AddOptions{URL: "https://example.com/no2.bad"}); err == nil {
		t.Fatal("advanced silently fell back")
	}
	before := len(items)
	items, _ = m.Snapshot()
	if len(items) != before {
		t.Fatal("failed admission created task")
	}
	bad[0].Pattern = "*"
	if err := m.SetDirectoryRules(bad); err == nil {
		t.Fatal("accepted invalid replacement")
	}
	if _, _, err := m.AddContext(context.Background(), "https://example.com/no3.bad"); err == nil {
		t.Fatal("invalid edit overwrote existing rule")
	}
}

func TestDirectoryRulesKeepPathAcrossResponseCredentialsAndRestart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="server.pdf"`)
		fmt.Fprint(w, "payload")
	}))
	defer srv.Close()
	base := t.TempDir()
	archive := filepath.Join(base, "archives")
	docs := filepath.Join(base, "documents")
	list := []settings.DirectoryRule{{ID: "zip", Enabled: true, Kind: "extension", Pattern: "zip", Dir: archive}, {ID: "pdf", Enabled: true, Kind: "extension", Pattern: "pdf", Dir: docs}}
	m := New(Options{Dir: base, DirectoryRules: list})
	defer m.Shutdown()
	id := addOne(t, m, srv.URL+"/original.zip")
	items, _ := m.Snapshot()
	original := items[0].Path
	if err := os.WriteFile(filepath.Join(archive, "other.zip.part.meta"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	addOne(t, m, srv.URL+"/other.zip")
	items, _ = m.Snapshot()
	if items[1].FileName == "other.zip" {
		t.Fatal("sidecar reservation lost")
	}
	list[0].Dir = docs
	if err := m.SetDirectoryRules(list); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddTask(AddOptions{URL: srv.URL + "/original.zip", Headers: http.Header{"X-Token": {"new"}}}); err != nil {
		t.Fatal(err)
	}
	items, _ = m.Snapshot()
	if items[0].Path != original {
		t.Fatal("credential refresh moved task")
	}
	records := m.Records()
	m.Shutdown()
	restored := New(Options{Dir: base, DirectoryRules: list})
	defer restored.Shutdown()
	restored.Restore(records)
	if _, err := restored.AddTask(AddOptions{URL: srv.URL + "/original.zip", Headers: http.Header{"X-Token": {"new"}}}); err != nil {
		t.Fatal(err)
	}
	restored.Resume(id)
	it := waitState(t, restored, id, StateCompleted)
	if filepath.Dir(it.Path) != archive || it.FileName != "server.pdf" {
		t.Fatalf("response/rules moved directory: %+v", it)
	}
}
