package app

import (
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
	"testing"
)

func TestAdvancedTaskRateValidation(t *testing.T) {
	engine := &fakeEngine{}
	a := newTestApp(t, engine)
	a.openAdvanced()
	a.advanced.url = "https://example.com/rate"
	a.advanced.rateKiB = "-1"
	tt := testerFor(a)
	if err := tt.Click("创建下载"); err != nil {
		t.Fatal(err)
	}
	if len(engine.items) != 0 || !a.advanced.open || a.advanced.err == "" {
		t.Fatal("invalid rate created a task")
	}
	a.advanced.rateKiB = "128"
	tt.Frame()
	if err := tt.Click("创建下载"); err != nil {
		t.Fatal(err)
	}
	if len(engine.items) != 1 || engine.items[0].TaskRateLimit != 128*1024 {
		t.Fatal("advanced rate was not submitted")
	}
}

func TestDetailsTaskRateEditing(t *testing.T) {
	it := seedItem("rate", "rate.bin", download.StateDownloading)
	it.TaskRateLimit = 1024
	engine := &fakeEngine{items: []download.Item{it}}
	a := newTestApp(t, engine)
	a.openDetails("rate")
	tt := testerFor(a)
	if a.detailsRate != "1" {
		t.Fatal("details did not initialize from task")
	}
	if err := tt.Click("任务限速 KB/s"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("abc")
	tt.Frame()
	if err := tt.Click("应用限速"); err != nil {
		t.Fatal(err)
	}
	if a.detailsRateErr == "" || engine.items[0].TaskRateLimit != 1024 {
		t.Fatal("invalid rate overwrote effective limit")
	}
	if err := tt.Click("任务限速 KB/s"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("64")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if a.detailsRateErr != "" || engine.items[0].TaskRateLimit != 64*1024 || !engine.hasCall("SetTaskRateLimit:rate:65536") {
		t.Fatal("rate edit did not immediately reach engine")
	}
	tt.Frame()
	if a.detailsRate != "64" {
		t.Fatal("rebuild lost edit")
	}
	if engine.Records()[0].TaskRateLimit != 65536 {
		t.Fatal("UI rate absent from persisted record")
	}
	a.detailsOpen = false
	engine.items[0].TaskRateLimit = 0
	a.openDetails("rate")
	tt.Frame()
	if a.detailsRate != "0" {
		t.Fatal("reopen retained stale draft")
	}
}
