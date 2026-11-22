package app

import (
	"fmt"
	"github.com/egoist/mygo"

	"go-downloader/internal/appinfo"
)

// Tray and window adapters are main-thread-owned. No UI build value or Context
// is retained here. Listeners live until the tray's one-time shutdown.
type trayHandle interface {
	SetMenu(*mygo.Menu)
	SetToolTip(string)
	OnClick(func()) func()
	Destroy()
}

type backgroundWindow interface {
	Hide()
	Show()
	Focus()
	IsMinimized() bool
	Restore()
	OnClose(func(*mygo.CloseEvent)) func()
}

type trayState struct {
	hidden                               bool
	handle                               trayHandle
	window                               backgroundWindow
	initialized, quitting, sessionEnding bool
	reason                               string
	off                                  []func()
}

func trayPlatformReason(goos string) string {
	if goos == "windows" {
		return "Windows 托盘尚待原生验收，关闭窗口将正常退出"
	}
	return "此平台尚未完成托盘恢复验收，关闭窗口将正常退出"
}

// initTray is called once after WhenReady. A creation error must leave ordinary
// window closing available, even when an older settings file enables the option.
func (a *app) initTray() {
	if a.tray.initialized {
		return
	}
	a.tray.initialized = true
	a.tray.reason = a.deps.trayUnavailable
	if a.tray.reason != "" {
		return
	}
	if a.deps.newTray == nil {
		a.tray.reason = "托盘服务不可用，关闭窗口将正常退出"
		return
	}
	handle, err := a.deps.newTray()
	if err != nil || handle == nil {
		a.tray.reason = "无法创建托盘图标，关闭窗口将正常退出"
		return
	}
	a.tray.handle = handle
	a.tray.off = append(a.tray.off, handle.OnClick(a.restoreWindow))
	a.refreshTray()
}

// refreshTray runs on the UI thread, including while its window is hidden.
// The independent saver ticker calls it at most two seconds after a change.
func (a *app) refreshTray() {
	if a.tray.handle == nil || a.stopping {
		return
	}
	_, summary := a.engine.Snapshot()
	speed := formatSpeed(summary.Speed)
	if speed == "" {
		speed = "0 B/s"
	}
	label := fmt.Sprintf("活动 %d · 排队 %d · %s", max(0, summary.Active-summary.Queued), summary.Queued, speed)
	a.tray.handle.SetToolTip(appinfo.Name + " · " + label)
	a.tray.handle.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{ID: "restore", Label: "恢复窗口", Click: func(*mygo.MenuItem, *mygo.Window) { a.restoreWindow() }},
		{ID: "summary", Label: label, Disabled: true}, mygo.Separator(),
		{ID: "pause", Label: "全部暂停", Click: func(*mygo.MenuItem, *mygo.Window) {
			if !a.stopping {
				a.engine.PauseAll()
				a.refreshTray()
			}
		}},
		{ID: "resume", Label: "全部继续", Click: func(*mygo.MenuItem, *mygo.Window) {
			if !a.stopping {
				a.engine.ResumeAll()
				a.refreshTray()
			}
		}}, mygo.Separator(),
		{ID: "quit", Label: "退出" + appinfo.Name, Click: func(*mygo.MenuItem, *mygo.Window) { a.requestQuit() }},
	}))
}

func (a *app) restoreWindow() {
	if a.stopping || a.tray.window == nil {
		return
	}
	if a.tray.window.IsMinimized() {
		a.tray.window.Restore()
	}
	a.tray.window.Show()
	a.tray.window.Focus()
	a.tray.hidden = false
}

func (a *app) bindBackgroundWindow(window backgroundWindow) {
	a.tray.window = window
	if window != nil {
		a.tray.off = append(a.tray.off, window.OnClose(a.onWindowClose))
	}
}

func (a *app) onWindowClose(event *mygo.CloseEvent) {
	if a.stopping || a.tray.quitting || a.tray.sessionEnding || !a.s.CloseToTray || a.tray.handle == nil || a.tray.reason != "" || a.tray.window == nil {
		return
	}
	event.PreventDefault()
	a.tray.window.Hide()
	a.tray.hidden = true
}

func (a *app) requestQuit() {
	if a.tray.quitting || a.deps.quit == nil {
		return
	}
	a.tray.quitting = true
	a.deps.quit()
}

// BeforeQuit is cancelable, so ordinary quits only set the closing guard.
// OnQuit commits shutdown; OS session ending cannot be canceled and must flush
// synchronously before returning to the operating system.
func (a *app) beforeQuit(event *mygo.QuitEvent) {
	a.tray.quitting = true
	a.tray.sessionEnding = event.SessionEnding
	if event.SessionEnding {
		a.shutdown()
	}
}

func (a *app) quitCanceled() {
	if !a.tray.sessionEnding {
		a.tray.quitting = false
	}
}

func (a *app) stopTray() {
	for _, off := range a.tray.off {
		off()
	}
	a.tray.off = nil
	if a.tray.handle != nil {
		a.tray.handle.Destroy()
		a.tray.handle = nil
	}
}
