package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
	"strings"
	"testing"
)

type fakeClipboard struct {
	callbacks   []func()
	text        string
	reads, offs int
}

func (f *fakeClipboard) attach(a *app) {
	a.deps.watchClipboard = func(fn func()) func() { f.callbacks = append(f.callbacks, fn); return func() { f.offs++ } }
	a.deps.readClipboardText = func() ([]byte, error) { f.reads++; return []byte(f.text), nil }
}
func (f *fakeClipboard) change(text string) { f.text = text; f.callbacks[len(f.callbacks)-1]() }

func TestClipboardPreferenceAndListenerLifetime(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	f := &fakeClipboard{text: "https://example.com/preexisting"}
	f.attach(a)
	a.syncClipboard()
	if len(f.callbacks) != 0 || a.s.ClipboardPrompts {
		t.Fatal("default listener enabled")
	}
	a.setClipboardPrompts(true)
	if f.reads != 0 || len(f.callbacks) != 1 {
		t.Fatal("enable read existing content")
	}
	got, err := a.store.LoadSettings(a.deps.defaultDir)
	if err != nil || !got.ClipboardPrompts {
		t.Fatal("preference not saved", err)
	}
	f.change("https://example.com/new")
	if len(a.clipboard.pending) != 1 {
		t.Fatal("new event missed")
	}
	a.setClipboardPrompts(false)
	reads := f.reads
	f.callbacks[0]()
	if f.reads != reads || f.offs != 1 || a.clipboard.pending != nil {
		t.Fatal("late disabled callback read or retained text")
	}
	a.setClipboardPrompts(true)
	a.shutdown()
	reads = f.reads
	f.callbacks[1]()
	if f.reads != reads || f.offs != 2 || a.clipboard.pending != nil {
		t.Fatal("exit did not release watcher")
	}
}

func TestClipboardTextBoundaryAndExplicitURLs(t *testing.T) {
	reads := 0
	read := func() (transfer.Data, error) {
		reads++
		return transfer.TextData(strings.Repeat("x", maxClipboardBytes+1)), nil
	}
	if text, err := clipboardText(func() []transfer.Format { return []transfer.Format{transfer.PNG} }, read); err != nil || text != nil || reads != 0 {
		t.Fatal("non-text read")
	}
	if text, err := clipboardText(func() []transfer.Format { return []transfer.Format{transfer.Text} }, read); err != nil || text != nil || reads != 1 {
		t.Fatal("large data retained")
	}
	raw, canonical := parseClipboardLinks(context.Background(), []byte("example.com text ftp://example.com/a https://user:secret@example.com/a?token=private HTTPS://example.com/b https://example.com/a?token=private"), nil)
	if len(raw) != 2 || !strings.Contains(raw[0], "user:secret") || strings.Contains(canonical, "secret") || !strings.Contains(canonical, "token=private") {
		t.Fatal("incorrect normalization")
	}
	if raw, _ := parseClipboardLinks(context.Background(), make([]byte, maxClipboardBytes+1), nil); len(raw) != 0 {
		t.Fatal("parser over limit")
	}
	if raw, _ := parseClipboardLinks(context.Background(), []byte("https://example.com/%zz"), nil); len(raw) != 0 {
		t.Fatal("invalid URL accepted")
	}
	if raw, _ := parseClipboardLinks(context.Background(), []byte("https://example.com/a"), []download.Item{{URL: "https://example.com/a", State: download.StateCompleted}}); len(raw) != 0 {
		t.Fatal("completed task prompted again")
	}
}

func TestClipboardFingerprintLRU(t *testing.T) {
	c := clipboardCache{}
	keys := make([][32]byte, 257)
	for i := range keys {
		keys[i] = c.fingerprint("links", fmt.Sprint(i))
		if i < 256 {
			c.remember(keys[i])
		}
	}
	if !c.seen(keys[0]) {
		t.Fatal("missing cached key")
	}
	c.remember(keys[256])
	if len(c.recent) != 256 || c.seen(keys[1]) || !c.seen(keys[0]) {
		t.Fatal("LRU eviction wrong")
	}
	other := clipboardCache{}
	if other.fingerprint("links", "0") == keys[0] {
		t.Fatal("fingerprints not salted per session")
	}
}

func TestClipboardPromptFocusIgnoreAuthAndSelfCopy(t *testing.T) {
	engine := &fakeEngine{}
	a := newTestApp(t, engine)
	f := &fakeClipboard{}
	f.attach(a)
	a.setClipboardPrompts(true)
	tt := testerFor(a)
	if err := tt.Click("下载链接输入"); err != nil {
		t.Fatal(err)
	}
	tt.Type("draft")
	secret := "https://user:secret@example.com/a?token=private"
	f.change(secret)
	tt.Frame()
	tt.Type(" stays")
	if len(engine.items) != 0 || a.input != "draft stays" || !tt.HasText("剪贴板发现 1 个新链接") {
		t.Fatal("prompt stole focus or auto-added")
	}
	if strings.Contains(strings.Join(tt.Texts(), " "), "secret") || strings.Contains(strings.Join(tt.Texts(), " "), "private") {
		t.Fatal("prompt leaked credentials")
	}
	if err := tt.Click("忽略"); err != nil {
		t.Fatal(err)
	}
	f.change(secret)
	tt.Frame()
	if len(a.clipboard.pending) != 0 {
		t.Fatal("ignored set prompted again")
	}
	a.suppressClipboardCopy("https://example.com/self")
	f.change("https://example.com/self")
	if len(a.clipboard.pending) != 0 {
		t.Fatal("self copy loop")
	}
	f.change(secret + "-renewed")
	tt.Frame()
	if err := tt.Click("添加剪贴板链接"); err != nil {
		t.Fatal(err)
	}
	if !engine.hasCall("Add:"+secret+"-renewed") || len(engine.items) != 1 {
		t.Fatal("confirmation lost memory authentication")
	}
	encoded, _ := json.Marshal(a.s)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "private") {
		t.Fatal("clipboard persisted in settings")
	}
}

func TestClipboardLatestHiddenCandidateAndStaleParsing(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	f := &fakeClipboard{}
	f.attach(a)
	a.setClipboardPrompts(true)
	var jobs []func()
	a.deps.async = func(fn func()) { jobs = append(jobs, fn) }
	a.tray.hidden = true
	f.change("https://example.com/old")
	f.change("https://example.com/latest")
	jobs[1]()
	jobs[0]()
	if len(a.clipboard.pending) != 1 || !strings.Contains(a.clipboard.pending[0], "latest") {
		t.Fatal("old parse replaced latest")
	}
	tt := testerFor(a)
	if tt.HasText("剪贴板发现") {
		t.Fatal("hidden window prompted")
	}
	a.tray.hidden = false
	tt.Frame()
	if !tt.HasText("剪贴板发现") {
		t.Fatal("restore missed latest")
	}
	f.change("https://example.com/late")
	a.setClipboardPrompts(false)
	jobs[2]()
	if len(a.clipboard.pending) != 0 {
		t.Fatal("disabled late parse published")
	}
	a.setClipboardPrompts(true)
	f.change("https://example.com/other")
	jobs[3]()
	tt.Frame()
	if err := tt.Click("下载链接输入"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("intact")
}
