// Package app implements the downloader's desktop application.
package app

import (
	"github.com/egoist/mygo"
	// MyGo's own controls, menus and dialogs in Simplified Chinese.
	_ "github.com/egoist/mygo/locales/zh-Hans"
)

// Run initializes and runs the desktop application on the main goroutine.
func Run() error {
	return run(realDeps)
}

// run keeps native startup on the caller's main goroutine. The dependency
// factory also permits isolated native acceptance runs without user data.
func run(makeDeps func() deps) error {
	if !mygo.App.RequestSingleInstanceLock() {
		return nil // the running instance comes to the front
	}
	// The app's texts are Chinese, so MyGo's own controls follow.
	mygo.App.SetLanguages("zh-Hans", "en")
	mygo.App.SetLocale("zh-Hans")
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
