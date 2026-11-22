package app

import (
	"encoding/json"
	"github.com/egoist/mygo"
	"go-downloader/internal/diagnostics"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticExportBusyCancelAndSnapshot(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	path := filepath.Join(t.TempDir(), "events.json")
	var jobs []func()
	a.deps.async = func(fn func()) { jobs = append(jobs, fn) }
	a.deps.saveDiagnostics = func(*mygo.Window) (string, error) { return path, nil }
	a.diagnostics.Record(diagnostics.NewEvent(diagnostics.HTTP, "one", "https://example.com/file?secret=hidden", 503, 0, nil))
	a.openSettings(3)
	tt := testerFor(a)
	if err := tt.Click("导出诊断日志"); err != nil {
		t.Fatal(err)
	}
	_ = tt.Click("导出诊断日志")
	if len(jobs) != 1 || !a.diagnosticExport.busy {
		t.Fatal("reentrant export")
	}
	jobs[0]()
	a.diagnostics.Record(diagnostics.NewEvent(diagnostics.Completed, "two", "", 0, 0, nil))
	jobs[1]()
	tt.Frame()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc diagnostics.Document
	if err = json.Unmarshal(data, &doc); err != nil || len(doc.Events) != 1 || doc.Events[0].TaskID != "one" || !tt.HasText("诊断日志已导出") {
		t.Fatal(doc, err)
	}
	if err := tt.Click("导出诊断日志"); err != nil {
		t.Fatal(err)
	}
	jobs[2]()
	if err := tt.Click("取消日志导出"); err != nil {
		t.Fatal(err)
	}
	jobs[3]()
	tt.Frame()
	got, _ := os.ReadFile(path)
	if string(got) != string(data) || a.diagnosticExport.busy || !tt.HasText("已取消导出") {
		t.Fatal("canceled export modified destination")
	}
	if err := tt.Click("导出诊断日志"); err != nil {
		t.Fatal(err)
	}
	a.cancelDiagnosticExport()
	jobs[4]()
	if len(jobs) != 5 {
		t.Fatal("late dialog started a write")
	}
}
