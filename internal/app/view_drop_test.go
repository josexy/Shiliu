package app

import (
	"context"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
	"go-downloader/internal/tasklist"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDropRepresentationPriorityAndRejection(t *testing.T) {
	data := transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.Text, []byte("https://example.com/wrong")),
		transfer.Bytes(transfer.URIList, []byte("# comment\r\nhttps://user:secret@example.com/right?token=hidden\r\n")),
	))
	in, err := captureDrop(data)
	if err != nil {
		t.Fatal(err)
	}
	p, err := tasklist.ReadBatch(context.Background(), in.texts, in.paths, nil)
	if err != nil || p.Valid != 1 || strings.Contains(p.Entries[0].URL, "secret") || strings.Contains(p.Entries[0].URL, "hidden") || !strings.Contains(p.Entries[0].URL, "right") {
		t.Fatal(p, err)
	}
	for _, data := range []transfer.Data{testURIData("ftp://example.com/a"), testURIData("file://remote/share/file.txt"), transfer.New(transfer.NewItem(transfer.Bytes(transfer.HTML, []byte("<a href='https://example.com'>link</a>"))))} {
		if _, err := captureDrop(data); err == nil {
			t.Fatal("unsupported representation accepted")
		}
	}
}

func TestFileDropPreviewOnceConfirmAndFocus(t *testing.T) {
	for _, auto := range []bool{false, true} {
		f := &fakeEngine{}
		a := newTestApp(t, f)
		a.setAutoStart(auto)
		path := filepath.Join(t.TempDir(), "links.txt")
		os.WriteFile(path, []byte("https://example.com/a.zip\nhttps://example.com/a.zip"), 0600)
		tt := testerFor(a)
		if err := tt.Click("下载链接输入"); err != nil {
			t.Fatal(err)
		}
		tt.Type("draft")
		r, ok := tt.Find("下载添加区")
		if !ok {
			t.Fatal("missing target")
		}
		if !tt.DropFiles(r.X+12, r.Y+12, path) {
			t.Fatal("file drop rejected")
		}
		for range 4 {
			tt.Frame()
		}
		if a.dropBatchID != 1 || len(f.items) != 0 || !tt.HasText("拖放链接预览") || a.taskList.preview.Valid != 1 {
			t.Fatal("drop duplicated or created before confirmation")
		}
		if err := tt.Click("确认导入"); err != nil {
			t.Fatal(err)
		}
		a.confirmImport()
		tt.Frame()
		if len(f.items) != 1 {
			t.Fatal("confirmation duplicated task")
		}
		want := download.StatePaused
		if auto {
			want = download.StateQueued
		}
		if f.items[0].State != want {
			t.Fatal("AutoStart ignored")
		}
		if err := tt.Click("关闭清单"); err != nil {
			t.Fatal(err)
		}
		tt.Type(" kept")
		if a.input != "draft kept" {
			t.Fatalf("input focus lost: %q", a.input)
		}
	}
}

func TestDropCancellationReplacementAndExitRejectLateResults(t *testing.T) {
	for _, mode := range []string{"cancel", "replace", "exit"} {
		t.Run(mode, func(t *testing.T) {
			a := newTestApp(t, &fakeEngine{})
			var work []func()
			a.deps.async = func(fn func()) { work = append(work, fn) }
			a.beginDrop(testURIData("https://example.com/old"))
			switch mode {
			case "cancel":
				a.closeTaskList()
			case "exit":
				a.stopping = true
				a.cancelLifetime()
				a.closeTaskList()
			case "replace":
				a.beginDrop(testURIData("https://example.com/new"))
				work[1]()
			}
			work[0]()
			if mode == "replace" {
				if a.taskList.preview.Valid != 1 || !strings.Contains(a.taskList.preview.Entries[0].URL, "new") {
					t.Fatal("old result replaced new batch")
				}
			} else if a.taskList.open {
				t.Fatal("old result reopened preview")
			}
		})
	}
}

func TestDropHoverHint(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	tt := ui.NewTester(func(c *ui.Context) { dropHint(c, a.palette, true) }, 700, 80)
	if !tt.HasText("确认后才会创建下载") {
		t.Fatal(tt.Texts())
	}
}

func testURIData(raw string) transfer.Data {
	return transfer.New(transfer.NewItem(transfer.Bytes(transfer.URIList, []byte(raw))))
}
