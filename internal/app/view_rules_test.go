package app

import (
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/settings"
	"testing"
)

func (f *fakeEngine) SetDirectoryRules([]settings.DirectoryRule) error { return nil }

func TestRuleEditorKeepsIdentityAndValidates(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	a.s.DirectoryRules = []settings.DirectoryRule{{ID: "first", Enabled: true, Kind: "extension", Pattern: "zip", Dir: a.s.DownloadDir}, {ID: "second", Enabled: true, Kind: "domain", Pattern: "example.com", Dir: a.s.DownloadDir}}
	a.openRules()
	tt := testerFor(a)
	if err := tt.Click("规则条件 first"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("pd")
	a.moveRule("first", 1)
	tt.Frame()
	tt.Type("f")
	if a.ruleEditor.rows[1].rule.Pattern != "pdf" || a.ruleEditor.rows[0].rule.ID != "second" {
		t.Fatal("reorder lost focus or field data")
	}
	a.ruleEditor.previewURL = "https://example.com/file.pdf"
	tt.Frame()
	if !tt.HasText("命中第 1 条（example.com）") {
		t.Fatal(tt.Texts())
	}
	a.ruleEditor.rows[0].rule.Pattern = "example.com:80"
	if err := tt.Click("保存规则"); err != nil {
		t.Fatal(err)
	}
	if a.ruleEditor.err == "" || a.s.DirectoryRules[0].ID != "first" {
		t.Fatal("bad edit replaced settings")
	}
	a.ruleEditor.rows[0].rule.Pattern = "example.com"
	tt.Frame()
	if err := tt.Click("保存规则"); err != nil {
		t.Fatal(err)
	}
	got, err := a.store.LoadSettings(a.deps.defaultDir)
	if err != nil || len(got.DirectoryRules) != 2 || got.DirectoryRules[0].ID != "second" || got.DirectoryRules[1].Pattern != "pdf" {
		t.Fatal(got, err)
	}
	a.openRules()
	tt.Frame()
	if err := tt.Click("删除规则 second"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("新增目录规则"); err != nil {
		t.Fatal(err)
	}
	if len(a.ruleEditor.rows) != 2 || a.ruleEditor.rows[0].rule.ID != "first" || a.ruleEditor.rows[1].rule.ID == "second" {
		t.Fatal("rule identity reused")
	}
}

func TestAdvancedDirectoryPrefillDoesNotOverrideRules(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	a.s.DirectoryRules = []settings.DirectoryRule{{ID: "zip", Enabled: true, Kind: "extension", Pattern: "zip", Dir: t.TempDir()}}
	a.openAdvanced()
	a.advanced.url = "https://example.com/file.zip"
	tt := testerFor(a)
	if a.advanced.dirOverride || !tt.HasText("命中第 1 条") {
		t.Fatal("default prefill bypassed rules")
	}
	if err := tt.Click("任务保存位置"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(t.TempDir())
	if !a.advanced.dirOverride || !tt.HasText("自定义目录：") {
		t.Fatal("typed directory not explicit")
	}
}

func TestQuickAddLateResultPreservesNewInput(t *testing.T) {
	f := &fakeEngine{}
	a := newTestApp(t, f)
	var work func()
	a.deps.async = func(fn func()) { work = fn }
	a.input = "https://example.com/old.zip"
	a.addDownloads()
	if !a.addBusy || work == nil {
		t.Fatal("quick admission was not dispatched")
	}
	a.input = "https://example.com/new.zip"
	work()
	if a.addBusy || a.input != "https://example.com/new.zip" || len(f.items) != 1 {
		t.Fatal("late result replaced new input")
	}
}
