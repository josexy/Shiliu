package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"go-downloader/internal/download"
	"go-downloader/internal/tasklist"
)

func importFixture(t *testing.T, a *app, raw string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "links.txt")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	a.deps.pickTaskList = func(*mygo.Window) (string, error) { return path, nil }
}

func TestImportUsesCurrentDeduplicationAndPathReservations(t *testing.T) {
	dir := t.TempDir()
	engine := download.New(download.Options{Dir: dir, AutoStart: false})
	defer engine.Shutdown()
	raw := "https://example.com/a/file.bin\nhttps://example.com/b/file.bin\nhttps://example.com/c/file.bin"
	preview, err := tasklist.Parse(context.Background(), []byte(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	// A separate entry point adds a task while the preview is open.
	engine.Add("https://example.com/a/file.bin")
	if err := os.WriteFile(filepath.Join(dir, "file (2).bin.part.meta"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	result := submitImportCandidates(context.Background(), engine, preview.Candidates())
	if result.added != 2 || result.skipped != 1 || result.failed != 0 {
		t.Fatalf("result: %+v", result)
	}
	items, _ := engine.Snapshot()
	want := []string{"file.bin", "file (3).bin", "file (4).bin"}
	for i, it := range items {
		if it.State != download.StatePaused || it.Path != filepath.Join(dir, want[i]) {
			t.Fatalf("bad admission: %+v", it)
		}
	}
}

func TestImportKeepsSuccessAndSummarizesCreationErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(bad, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := download.New(download.Options{Dir: dir})
	defer engine.Shutdown()
	engine.SetOnChange(func() { engine.SetDir(bad) })
	result := submitImportCandidates(context.Background(), engine, []string{"https://example.com/one", "https://user:secret@example.com/two?token=private"})
	if result.added != 1 || result.failed != 1 || len(result.errors) != 1 {
		t.Fatalf("partial result: %+v", result)
	}
	if strings.Contains(fmt.Sprint(result), "secret") || strings.Contains(fmt.Sprint(result), "private") {
		t.Fatal("error summary leaked a candidate")
	}
	items, _ := engine.Snapshot()
	if len(items) != 1 {
		t.Fatal("partial failure rolled back or admitted failed task")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = submitImportCandidates(ctx, engine, []string{"example.com/new"})
	if result.remaining != 1 || result.added != 0 {
		t.Fatal("cancel admitted tasks")
	}
}

func TestImportAuthenticationReachesLocalServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "user" || pass != "secret" {
			t.Error("import lost authentication")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "content")
	}))
	defer srv.Close()
	done := make(chan download.Item, 1)
	engine := download.New(download.Options{Dir: t.TempDir(), AutoStart: true, OnComplete: func(it download.Item) { done <- it }})
	defer engine.Shutdown()
	raw := strings.Replace(srv.URL, "http://", "http://user:secret@", 1) + "/file"
	p, err := tasklist.Parse(context.Background(), []byte(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	result := submitImportCandidates(context.Background(), engine, p.Candidates())
	if result.added != 1 || result.failed != 0 {
		t.Fatalf("result: %+v", result)
	}
	select {
	case it := <-done:
		data, err := os.ReadFile(it.Path)
		if err != nil || string(data) != "content" {
			t.Fatal("download content differs")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("imported download did not complete")
	}
}

func TestImportPreviewFocusScrollAndCancel(t *testing.T) {
	engine := &fakeEngine{}
	a := newTestApp(t, engine)
	var links strings.Builder
	for i := range 100 {
		fmt.Fprintf(&links, "https://user:secret@example.com/file%03d.bin?token=private\n", i)
	}
	links.WriteString("https://example.com/file000.bin?token=private\nftp://secret@host\n")
	importFixture(t, a, links.String())
	tt := testerFor(a)
	tt.SetSize(960, 640)
	tt.SetScale(1.25)
	if err := tt.Click("导入 TXT"); err != nil {
		t.Fatal(err)
	}
	// Opening the modal focuses the file picker button; extra builds retain it.
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if !tt.HasText("有效 100 · 重复 1 · 无效 1") {
		t.Fatalf("preview: %q", tt.Texts())
	}
	if strings.Contains(strings.Join(tt.Texts(), " "), "secret") || strings.Contains(strings.Join(tt.Texts(), " "), "private") {
		t.Fatal("preview exposed credentials")
	}
	if len(engine.items) != 0 {
		t.Fatal("preview created tasks")
	}
	for _, label := range []string{"选择 TXT…", "确认导入", "关闭任务清单"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 960 || r.Y+r.H > 640 {
			t.Fatalf("control outside compact window: %s %+v", label, r)
		}
	}
	first := "https://example.com/file000.bin"
	if !tt.HasText(first) {
		t.Fatal("first preview row missing")
	}
	tt.Wheel(600, 320, 0, 700)
	tt.Advance(500 * time.Millisecond)
	if tt.HasText(first) {
		t.Fatal("preview did not scroll")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.taskList.open || len(a.taskList.preview.Candidates()) != 0 || len(engine.items) != 0 {
		t.Fatal("cancel retained candidates or created tasks")
	}
}

func TestImportRejectsLateResultsAndDuplicateConfirm(t *testing.T) {
	engine := &fakeEngine{}
	a := newTestApp(t, engine)
	importFixture(t, a, "example.com/a\nexample.com/b")
	var jobs []func()
	a.deps.async = func(fn func()) { jobs = append(jobs, fn) }
	a.openImport()
	a.chooseTaskList()
	jobs[0]()
	jobs = jobs[1:] // picker result schedules file read
	a.closeTaskList()
	a.openImport()
	jobs[0]()
	jobs = nil
	if len(a.taskList.preview.Entries) != 0 || a.taskList.busy {
		t.Fatal("late reader changed a newer modal")
	}
	a.taskList.preview, _ = tasklist.Parse(context.Background(), []byte("example.com/a\nexample.com/b"), nil)
	tt := testerFor(a)
	if err := tt.Click("确认导入"); err != nil {
		t.Fatal(err)
	}
	a.confirmImport()
	tt.Frame()
	if len(jobs) != 1 {
		t.Fatalf("duplicate submission: %d", len(jobs))
	}
	jobs[0]()
	jobs = nil
	tt.Frame()
	a.confirmImport()
	if len(engine.items) != 2 || len(jobs) != 0 || !tt.HasText("已添加 2 项") {
		t.Fatal("import did not commit exactly once")
	}
	if len(a.taskList.preview.Candidates()) != 0 {
		t.Fatal("finished import retained credentials")
	}
}

func TestExportSelectionCancelAndSanitization(t *testing.T) {
	items := sampleItems()
	items[0].URL = "https://user:secret@example.com/file?signature=private#hidden"
	a := newTestApp(t, &fakeEngine{items: items})
	a.selection.Add(items[0].ID)
	a.deps.saveTaskList = func(*mygo.Window) (string, error) { return "", nil }
	a.openExport()
	tt := testerFor(a)
	if !tt.HasText("清单不是备份") {
		t.Fatal("sharing limitation missing")
	}
	if err := tt.Click("导出所选"); err != nil {
		t.Fatal(err)
	}
	if a.taskList.busy || a.taskList.finished {
		t.Fatal("cancelled picker saved a file")
	}
	path := filepath.Join(t.TempDir(), "share.json")
	a.deps.saveTaskList = func(*mygo.Window) (string, error) { return path, nil }
	if err := tt.Click("导出所选"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private") || strings.Contains(string(data), "secret") || strings.Contains(string(data), "path") || strings.Count(string(data), "fileName") != 1 {
		t.Fatalf("unsafe selection export: %s", data)
	}
	if !tt.HasText("脱敏任务清单已保存") {
		t.Fatal("export completion missing")
	}
}
