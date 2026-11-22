package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
	"go-downloader/internal/proxy"
)

type fakeTray struct {
	menu                    *mygo.Menu
	tip                     string
	click                   func()
	destroyed, unsubscribed int
}

func (t *fakeTray) SetMenu(m *mygo.Menu) { t.menu = m }
func (t *fakeTray) SetToolTip(s string)  { t.tip = s }
func (t *fakeTray) OnClick(fn func()) func() {
	t.click = fn
	return func() { t.click = nil; t.unsubscribed++ }
}
func (t *fakeTray) Destroy() { t.destroyed++ }

func TestTrayCreationFailureAndOldSettings(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	if a.s.CloseToTray {
		t.Fatal("old settings changed closing behavior")
	}
	a.s.CloseToTray = true
	window := &fakeBackgroundWindow{}
	a.bindBackgroundWindow(window)
	calls := 0
	a.deps.newTray = func() (trayHandle, error) { calls++; return nil, errors.New("unavailable") }
	a.initTray()
	a.initTray()
	if calls != 1 || a.tray.handle != nil || a.tray.reason == "" {
		t.Fatal("tray failure did not disable background mode")
	}
	event := &mygo.CloseEvent{}
	window.close(event)
	if event.DefaultPrevented() || window.hidden {
		t.Fatal("tray failure prevented normal window closing")
	}
	defer a.stopTray()
	if err := a.store.SaveSettings(a.s); err != nil {
		t.Fatal(err)
	}
	s, err := a.store.LoadSettings(a.s.DownloadDir)
	if err != nil || !s.CloseToTray {
		t.Fatal("close-to-tray setting did not round trip")
	}
}

type fakeBackgroundWindow struct {
	hidden, minimized                      bool
	shown, focused, restored, unsubscribed int
	close                                  func(*mygo.CloseEvent)
}

func (w *fakeBackgroundWindow) Hide()             { w.hidden = true }
func (w *fakeBackgroundWindow) Show()             { w.hidden = false; w.shown++ }
func (w *fakeBackgroundWindow) Focus()            { w.focused++ }
func (w *fakeBackgroundWindow) IsMinimized() bool { return w.minimized }
func (w *fakeBackgroundWindow) Restore()          { w.minimized = false; w.restored++ }
func (w *fakeBackgroundWindow) OnClose(fn func(*mygo.CloseEvent)) func() {
	w.close = fn
	return func() { w.close = nil; w.unsubscribed++ }
}

func TestTrayMenuRefreshAndLifetime(t *testing.T) {
	engine := &fakeEngine{items: sampleItems()}
	a := newTestApp(t, engine)
	tray := &fakeTray{}
	window := &fakeBackgroundWindow{hidden: true, minimized: true}
	created, quits := 0, 0
	a.deps.newTray = func() (trayHandle, error) { created++; return tray, nil }
	a.deps.quit = func() { quits++ }
	a.tray.window = window
	a.initTray()
	a.initTray()
	if created != 1 || tray.menu == nil || !strings.Contains(tray.tip, "活动 2 · 排队 1") {
		t.Fatalf("tray: %+v", tray)
	}
	click := func(id string) {
		t.Helper()
		for _, it := range tray.menu.Items() {
			if it.ID == id {
				it.Click(it, nil)
				return
			}
		}
		t.Fatalf("missing menu %s", id)
	}
	click("pause")
	if !engine.hasCall("PauseAll") || !strings.Contains(tray.tip, "活动 0 · 排队 0") {
		t.Fatal("hidden pause did not refresh without a UI frame")
	}
	click("resume")
	if !engine.hasCall("ResumeAll") {
		t.Fatal("resume missing")
	}
	click("restore")
	if window.hidden || window.shown != 1 || window.focused != 1 || window.restored != 1 {
		t.Fatal("restore lost native window state")
	}
	click("quit")
	click("quit")
	if quits != 1 {
		t.Fatal("quit submitted twice")
	}
	a.stopTray()
	a.stopTray()
	if tray.destroyed != 1 || tray.unsubscribed != 1 || tray.click != nil {
		t.Fatal("tray resources not released exactly once")
	}
}

func TestCloseToTrayPreservesTasksAndWindow(t *testing.T) {
	engine := &fakeEngine{items: sampleItems()}
	a := newTestApp(t, engine)
	tray := &fakeTray{}
	window := &fakeBackgroundWindow{}
	a.deps.newTray = func() (trayHandle, error) { return tray, nil }
	a.bindBackgroundWindow(window)
	a.initTray()
	a.selection.Add("d1")
	for _, enabled := range []bool{false, true} {
		a.s.CloseToTray = enabled
		event := &mygo.CloseEvent{}
		window.close(event)
		if event.DefaultPrevented() != enabled || window.hidden != enabled {
			t.Fatalf("closing with CloseToTray=%v failed", enabled)
		}
	}
	if engine.hasCall("Shutdown") || engine.hasCall("PauseAll") {
		t.Fatal("hiding stopped downloads")
	}
	for range 2 {
		a.restoreWindow()
	}
	if a.tray.window != window || window.hidden || window.shown != 2 || window.focused != 2 || a.selection.Len() != 1 {
		t.Fatal("restore replaced the window or selection")
	}
	a.stopTray()
	if window.unsubscribed != 1 {
		t.Fatal("window listener leaked")
	}
	for _, reason := range []string{"unavailable", ""} {
		a.tray.reason = reason
		event := &mygo.CloseEvent{}
		a.onWindowClose(event)
		if event.DefaultPrevented() {
			t.Fatal("unusable tray trapped the window")
		}
	}
}

func TestQuitCancellationKeepsEngineUsable(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	engine := download.New(download.Options{Dir: a.s.DownloadDir})
	a.engine = engine
	defer a.shutdown()
	tray := &fakeTray{}
	window := &fakeBackgroundWindow{}
	a.deps.newTray = func() (trayHandle, error) { return tray, nil }
	a.bindBackgroundWindow(window)
	a.initTray()
	a.s.CloseToTray = true
	a.beforeQuit(&mygo.QuitEvent{})
	event := &mygo.CloseEvent{}
	a.onWindowClose(event)
	if event.DefaultPrevented() || window.hidden {
		t.Fatal("quit was intercepted as close-to-tray")
	}
	a.quitCanceled()
	if a.tray.quitting || a.stopping {
		t.Fatal("canceled quit retained shutdown state")
	}
	if added, _, err := engine.AddContext(context.Background(), "example.com/after-cancel"); err != nil || added != 1 {
		t.Fatal("canceled quit closed the engine")
	}
	event = &mygo.CloseEvent{}
	a.onWindowClose(event)
	if !event.DefaultPrevented() {
		t.Fatal("canceled quit did not restore close-to-tray")
	}
}

func TestCommittedAndSessionQuitCancelWorkAndSaveOnce(t *testing.T) {
	for _, sessionEnding := range []bool{false, true} {
		t.Run(fmt.Sprint(sessionEnding), func(t *testing.T) {
			a := newTestApp(t, &fakeEngine{})
			engine := download.New(download.Options{Dir: a.s.DownloadDir})
			a.engine = engine
			engine.Add("example.com/paused")
			tray := &fakeTray{}
			window := &fakeBackgroundWindow{}
			a.deps.newTray = func() (trayHandle, error) { return tray, nil }
			a.bindBackgroundWindow(window)
			a.initTray()
			a.s.CloseToTray = true
			a.openImport()
			ctx := a.taskList.ctx
			a.taskListJobs.Add(1)
			go func() { defer a.taskListJobs.Done(); <-ctx.Done() }()
			a.beforeQuit(&mygo.QuitEvent{SessionEnding: sessionEnding})
			if !sessionEnding && a.stopping {
				t.Fatal("cancelable stage shut down")
			}
			a.shutdown()
			if ctx.Err() == nil || tray.destroyed != 1 || window.unsubscribed != 1 {
				t.Fatal("quit did not cancel jobs and native listeners")
			}
			first, err := os.Stat(a.store.TasksPath())
			if err != nil {
				t.Fatal(err)
			}
			a.shutdown()
			a.beforeQuit(&mygo.QuitEvent{SessionEnding: sessionEnding})
			second, err := os.Stat(a.store.TasksPath())
			if err != nil || !first.ModTime().Equal(second.ModTime()) {
				t.Fatal("shutdown saved twice")
			}
			if _, _, err := engine.AddContext(context.Background(), "example.com/after-quit"); err == nil {
				t.Fatal("quit left engine accepting work")
			}
			if _, err := a.store.LoadTasks(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("job did not cancel")
			}
		})
	}
}

func TestShutdownCancelsProxyBackgroundJobs(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	a.deps.async = func(fn func()) { go fn() }
	started := make(chan struct{}, 2)
	a.deps.detectSystem = func(ctx context.Context) (*proxy.System, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	a.deps.testProxy = func(ctx context.Context, _ proxy.Config, _ *proxy.System, _ string) (time.Duration, error) {
		started <- struct{}{}
		<-ctx.Done()
		return 0, ctx.Err()
	}
	a.refreshSystemAsync()
	a.runProxyTest()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("background job did not start")
		}
	}
	start := time.Now()
	a.shutdown()
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("shutdown waited for proxy timeouts instead of canceling")
	}
}

func TestTrayRuntimeAvailability(t *testing.T) {
	for _, test := range []struct {
		name      string
		create    func() (trayHandle, error)
		available bool
	}{
		{"created", func() (trayHandle, error) { return &fakeTray{}, nil }, true},
		{"creation error", func() (trayHandle, error) { return nil, errors.New("no tray") }, false},
		{"missing handle", func() (trayHandle, error) { return nil, nil }, false},
		{"missing service", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newTestApp(t, &fakeEngine{})
			a.s.CloseToTray = true
			a.deps.newTray = test.create
			window := &fakeBackgroundWindow{}
			a.bindBackgroundWindow(window)
			a.initTray()
			defer a.stopTray()
			event := &mygo.CloseEvent{}
			window.close(event)
			if (a.tray.handle != nil) != test.available || (a.tray.reason == "") != test.available {
				t.Fatalf("tray availability: handle=%v reason=%q", a.tray.handle, a.tray.reason)
			}
			if event.DefaultPrevented() != test.available || window.hidden != test.available {
				t.Fatalf("window closing: prevented=%v hidden=%v", event.DefaultPrevented(), window.hidden)
			}
		})
	}
}

func TestCloseToTraySettingPersistsAndRetainsFocus(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	a.deps.newTray = func() (trayHandle, error) { return &fakeTray{}, nil }
	a.initTray()
	a.openSettings(0)
	tt := testerFor(a)
	tt.SetSize(960, 640)
	tt.SetScale(1.25)
	tt.Wheel(600, 400, 0, 600)
	tt.Advance(500 * time.Millisecond)
	if err := tt.Click("关闭到托盘开关"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	stored, err := a.store.LoadSettings(a.s.DownloadDir)
	if err != nil || !stored.CloseToTray || !a.s.CloseToTray {
		t.Fatalf("toggle did not persist: stored=%v model=%v err=%v", stored.CloseToTray, a.s.CloseToTray, err)
	}
	tt.Key(0, ui.KeySpace)
	if a.s.CloseToTray {
		t.Fatal("keyed switch lost focus across frames")
	}
	a.s.CloseToTray = true
	a.saveSettings()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	a.openSettings(0)
	tt.Frame()
	if !a.s.CloseToTray {
		t.Fatal("reopening settings lost the value")
	}
	a.stopTray()
}

func TestTrayHostLossRestoresHiddenWindow(t *testing.T) {
	engine := &fakeEngine{items: sampleItems()}
	a := newTestApp(t, engine)
	a.s.CloseToTray = true
	a.deps.newTray = func() (trayHandle, error) { return &fakeTray{}, nil }
	window := &fakeBackgroundWindow{}
	a.bindBackgroundWindow(window)
	a.initTray()
	defer a.stopTray()
	for _, available := range []bool{false, true, false, true} {
		a.setTrayHostAvailable(available)
		if window.hidden {
			t.Fatal("host change left the window hidden")
		}
		event := &mygo.CloseEvent{}
		window.close(event)
		if event.DefaultPrevented() != available || window.hidden != available {
			t.Fatalf("host=%v: prevented=%v hidden=%v", available, event.DefaultPrevented(), window.hidden)
		}
	}
	if window.shown != 1 || window.focused != 1 || engine.hasCall("PauseAll") || engine.hasCall("Shutdown") {
		t.Fatal("host loss must restore the original window without stopping downloads")
	}
	a.stopTray()
	a.setTrayHostAvailable(false)
	if window.shown != 1 {
		t.Fatal("late host update restored a stopped tray")
	}
}

func TestTrayHostMonitorPendingAndCancellation(t *testing.T) {
	a := newTestApp(t, &fakeEngine{})
	a.s.CloseToTray = true
	a.deps.newTray = func() (trayHandle, error) { return &fakeTray{}, nil }
	var worker func()
	a.deps.async = func(fn func()) { worker = fn }
	a.deps.trayHostAvailable = func(context.Context) bool {
		t.Fatal("canceled monitor called the host probe")
		return true
	}
	window := &fakeBackgroundWindow{}
	a.bindBackgroundWindow(window)
	a.initTray()
	if worker == nil || a.tray.reason == "" {
		t.Fatal("host detection was not scheduled")
	}
	event := &mygo.CloseEvent{}
	window.close(event)
	if event.DefaultPrevented() || window.hidden {
		t.Fatal("unconfirmed host allowed hiding the window")
	}
	a.stopTray()
	worker()
	a.backgroundJobs.Wait()
}
