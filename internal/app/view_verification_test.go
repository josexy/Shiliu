package app

import (
	"github.com/egoist/mygo/ui"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-downloader/internal/download"
)

func TestCompletionNotificationDoesNotClaimVerified(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	a.canNotify = true
	a.deps.appActive = func() bool { return false }
	var title, body string
	a.deps.notify = func(t, b string) { title, body = t, b }
	a.onEngineComplete(download.Item{FileName: "file.bin", State: download.StateCompleted, ExpectedSHA256: strings.Repeat("0", 64), VerificationState: download.VerificationPending})
	if title != "传输完成" || !strings.Contains(body, "校验结果请查看") || strings.Contains(body, "校验通过") {
		t.Fatal("transfer notification claims integrity success")
	}
}

func TestVerificationDetailsStatesAndControls(t *testing.T) {
	for _, state := range []download.VerificationState{download.VerificationUnconfigured, download.VerificationPending, download.VerificationChecking, download.VerificationMatched, download.VerificationMismatched, download.VerificationCanceled, download.VerificationFailed} {
		t.Run(string(state), func(t *testing.T) {
			it := seedItem("verify", "file.bin", download.StateCompleted)
			it.VerificationState = state
			if state != download.VerificationUnconfigured {
				it.ExpectedSHA256 = strings.Repeat("a", 64)
			}
			it.VerificationDone = 512
			it.VerificationTotal = 1024
			engine := &fakeEngine{items: []download.Item{it}}
			a := newTestApp(t, engine)
			a.openDetails(it.ID)
			tt := testerFor(a)
			if !tt.HasText("完整性：" + verificationLabel(state)) {
				t.Fatalf("missing state %s", state)
			}
			if state == download.VerificationChecking {
				if !tt.HasText("已校验") {
					t.Fatal("checking progress missing")
				}
				if err := tt.Click("取消校验"); err != nil {
					t.Fatal(err)
				}
				if !engine.hasCall("CancelVerification:verify") {
					t.Fatal("cancel not dispatched")
				}
			} else if state != download.VerificationUnconfigured {
				if err := tt.Click("重新校验"); err != nil {
					t.Fatal(err)
				}
				if !engine.hasCall("Verify:verify") {
					t.Fatal("verify not dispatched")
				}
			}
		})
	}
}

func TestSHAFormInputAndEditing(t *testing.T) {
	engine := &fakeEngine{}
	a := newTestApp(t, engine)
	a.openAdvanced()
	a.advanced.url = "https://example.com/hash"
	a.advanced.expectedSHA = "bad"
	tt := testerFor(a)
	if err := tt.Click("创建下载"); err != nil {
		t.Fatal(err)
	}
	if !a.advanced.open || len(engine.items) > 0 || a.advanced.err == "" {
		t.Fatal("invalid SHA created task")
	}
	a.advanced.expectedSHA = strings.Repeat("AB", 32)
	tt.Frame()
	if err := tt.Click("创建下载"); err != nil {
		t.Fatal(err)
	}
	if engine.items[0].ExpectedSHA256 != strings.Repeat("ab", 32) {
		t.Fatal("SHA not normalized")
	}
	a.openDetails(engine.items[0].ID)
	tt.Frame()
	if err := tt.Click("预期 SHA-256"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("invalid")
	tt.Frame()
	if err := tt.Click("保存摘要"); err != nil {
		t.Fatal(err)
	}
	if a.detailsSHAErr == "" || engine.items[0].ExpectedSHA256 != strings.Repeat("ab", 32) {
		t.Fatal("invalid edit overwrote digest")
	}
	if err := tt.Click("预期 SHA-256"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(strings.Repeat("CD", 32))
	tt.Key(0, ui.KeyEnter)
	if a.detailsSHAErr != "" || engine.items[0].ExpectedSHA256 != strings.Repeat("cd", 32) {
		t.Fatal("valid edit not applied")
	}
	a.openDetails(engine.items[0].ID)
	tt.Frame()
	if a.detailsSHA != strings.Repeat("cd", 32) {
		t.Fatal("reopen lost digest")
	}
}

func TestRestartDisplaysHistoricalMatchAsPending(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	path := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(path, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	m := download.New(download.Options{Dir: filepath.Dir(path)})
	defer m.Shutdown()
	m.Restore([]download.Record{{ID: "history", URL: "https://example.com/file", Path: path, State: download.StateCompleted, ExpectedSHA256: strings.Repeat("a", 64), LastVerification: download.VerificationResult{State: download.VerificationMatched, CheckedAt: testNow, ActualSHA256: strings.Repeat("a", 64)}}})
	a.engine = m
	a.openDetails("history")
	tt := testerFor(a)
	if !tt.HasText("完整性：待校验") || !tt.HasText("上次结果：匹配") {
		t.Fatal("history shown as a current match")
	}
}
