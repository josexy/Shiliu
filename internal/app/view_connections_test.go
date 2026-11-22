package app

import (
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
	"testing"
)

func TestConnectionSettingsValidationAndPersistence(t *testing.T) {
	f := &fakeEngine{}
	a := newTestApp(t, f)
	a.openSettings(0)
	tt := testerFor(a)
	if err := tt.Click("全局连接上限"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("65")
	tt.Key(0, ui.KeyEnter)
	if a.connectionsErr == "" || f.hasCall("SetMaxConnections:") {
		t.Fatal("invalid global limit applied")
	}
	if err := tt.Click("全局连接上限"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("2")
	if err := tt.Click("默认任务连接数"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("4")
	tt.Key(0, ui.KeyEnter)
	if a.connectionsErr != "" || a.s.MaxConnections != 2 || a.s.TaskConnections != 4 || !f.hasCall("SetMaxConnections:2") || !f.hasCall("SetDefaultConnections:4") {
		t.Fatal("valid settings not applied")
	}
	a.settingsOpen = false
	a.openAdvanced()
	tt.Frame()
	if a.advanced.connections != "4" {
		t.Fatal("advanced form missed default")
	}
	a.advanced.url = "https://example.com/file"
	a.advanced.connections = "1.5"
	tt.Frame()
	if err := tt.Click("创建下载"); err != nil {
		t.Fatal(err)
	}
	if a.advanced.err == "" || len(f.items) != 0 {
		t.Fatal("fractional connection count accepted")
	}
	a.advanced.connections = "3"
	tt.Frame()
	if err := tt.Click("创建下载"); err != nil {
		t.Fatal(err)
	}
	if f.items[0].ConnectionLimit != 3 {
		t.Fatal("task limit not passed")
	}
}

func TestConnectionDetailsWaitingFallbackAndEdit(t *testing.T) {
	it := seedItem("multi", "file.bin", download.StateDownloading)
	it.TransferMode = "segments"
	it.ConnectionLimit = 4
	it.ActiveConnections = 2
	it.WaitingConnections = 2
	f := &fakeEngine{items: []download.Item{it}}
	a := newTestApp(t, f)
	a.openDetails(it.ID)
	tt := testerFor(a)
	if !tt.HasText("多连接分片 · 活动 2 · 等待 2 · 任务上限 4") {
		t.Fatal("connection state missing")
	}
	if err := tt.Click("详情任务连接数"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("9")
	tt.Key(0, ui.KeyEnter)
	if a.detailsConnectionsErr == "" || f.items[0].ConnectionLimit != 4 {
		t.Fatal("invalid task edit applied")
	}
	if err := tt.Click("详情任务连接数"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("1")
	tt.Key(0, ui.KeyEnter)
	if a.detailsConnectionsErr != "" || f.items[0].ConnectionLimit != 1 {
		t.Fatal("valid task edit missed")
	}
	f.items[0].TransferMode = "single"
	f.items[0].FallbackReason = "文件小于 8 MiB，使用单连接"
	tt.Frame()
	if !tt.HasText("文件小于 8 MiB") {
		t.Fatal("fallback reason missing")
	}
}
