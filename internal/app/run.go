// Package app implements the downloader's desktop application.
package app

import (
	"github.com/egoist/mygo"
	// MyGo's own controls, menus and dialogs in Simplified Chinese.
	_ "github.com/egoist/mygo/locales/zh-Hans"
	updaterNative "github.com/egoist/mygo/plugins/updater/native"

	"go-downloader/internal/appinfo"
)

// Run initializes and runs the desktop application on the main goroutine.
func Run() error {
	// Plain go run builds also have a stable name and stay apart from releases.
	if !mygo.App.IsPackaged() {
		mygo.App.SetName(appinfo.Name + " Local")
	}
	return run(realDeps)
}

// run keeps native startup on the caller's main goroutine. The dependency
// factory also permits isolated native acceptance runs without user data.
func run(makeDeps func() deps) error {
	useLegacyDataDir()
	if !mygo.App.RequestSingleInstanceLock() {
		return nil // the running instance comes to the front
	}
	// The app's texts are Chinese, so MyGo's own controls follow.
	mygo.App.SetLanguages("zh-Hans", "en")
	mygo.App.SetLocale("zh-Hans")
	// The native updater keeps updates independent of a webview runtime.
	mygo.Use(updaterNative.Plugin)
	a := newApp(makeDeps())
	a.load()
	defer a.shutdown()
	mygo.Theme.SetSource(themeSource(a.s.Theme))
	mygo.App.OnSecondInstance(func([]string, string) {
		a.restoreWindow()
	})
	mygo.App.OnNotificationClick(func(string) {
		a.restoreWindow()
	})
	mygo.App.WhenReady(a.start)
	mygo.App.OnBeforeQuit(a.beforeQuit)
	mygo.App.OnQuitCanceled(a.quitCanceled)
	mygo.App.OnQuit(a.shutdown)
	return mygo.App.Run()
}
