package app

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSourceDialogFailureSaveAndExplicitContinue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(403)
			return
		}
		fmt.Fprint(w, "new data")
	}))
	defer server.Close()
	m := download.New(download.Options{Dir: t.TempDir()})
	defer m.Shutdown()
	id, err := m.AddTask(download.AddOptions{URL: server.URL + "/old", FileName: "fixed.bin"})
	if err != nil {
		t.Fatal(err)
	}
	before := m.Records()[0]
	if err := os.WriteFile(before.Path+".part", []byte("old prefix"), 0600); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, &fakeEngine{})
	a.engine = m
	a.openDetails(id)
	tt := testerFor(a)
	if err := tt.Click("更新下载链接…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("新下载链接"); err != nil {
		t.Fatal(err)
	}
	tt.Type(server.URL + "/bad")
	if err := tt.Click("验证新链接"); err != nil {
		t.Fatal(err)
	}
	if a.source.err == "" || a.source.draft != nil || m.Records()[0].URL != before.URL {
		t.Fatal("failed validation changed task")
	}
	data, _ := os.ReadFile(before.Path + ".part")
	if string(data) != "old prefix" {
		t.Fatal("failed validation altered partial")
	}
	if err := tt.Click("新下载链接"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(server.URL + "/new")
	if err := tt.Click("验证新链接"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("从头下载") {
		t.Fatal("reset explanation missing")
	}
	if err := tt.Click("保存新链接"); err != nil {
		t.Fatal(err)
	}
	if !a.source.saved || a.source.url != "" || a.source.headers != "" {
		t.Fatal("save did not clear credentials")
	}
	a.commitSource() // repeated action is ignored after commit
	record := m.Records()[0]
	if record.URL != server.URL+"/new" || record.State != download.StatePaused || record.Path != before.Path {
		t.Fatal("save started download or lost identity")
	}
	data, _ = os.ReadFile(before.Path + ".part")
	if string(data) != "old prefix" {
		t.Fatal("save reset before explicit continue")
	}
	if err := tt.Click("继续下载"); err != nil {
		t.Fatal(err)
	}
	if a.source.open {
		t.Fatal("continue left credential dialog open")
	}
	m.Shutdown()
}

func TestSourceDialogCancelAndStaleBackgroundResult(t *testing.T) {
	m := download.New(download.Options{Dir: t.TempDir()})
	defer m.Shutdown()
	id, err := m.AddTask(download.AddOptions{URL: "https://example.com/old"})
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, &fakeEngine{})
	a.engine = m
	a.openSource(id)
	a.source.url = "https://user:secret@example.com/new"
	a.source.headers = "Cookie: private=secret"
	var jobs []func()
	a.deps.async = func(fn func()) { jobs = append(jobs, fn) }
	tt := testerFor(a)
	if err := tt.Click("验证新链接"); err != nil {
		t.Fatal(err)
	}
	a.probeSource()
	if len(jobs) != 1 {
		t.Fatal("duplicate probe queued")
	}
	ctx := a.source.ctx
	if err := tt.Click("关闭更新"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled || a.source.url != "" || a.source.headers != "" || a.source.draft != nil {
		t.Fatal("cancel retained credentials")
	}
	a.openSource(id)
	jobs[0]() // canceled before HTTP; late result cannot overwrite reopened form
	tt.Frame()
	if a.source.err != "" || a.source.busy || a.source.preview.URL != "" {
		t.Fatal("stale result published")
	}
	a.source.url = "https://example.com/new"
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.source.open || a.source.url != "" {
		t.Fatal("Escape did not discard draft")
	}
}
