package app

import (
	"context"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"

	"go-downloader/internal/appinfo"
	"go-downloader/internal/proxy"
	"go-downloader/internal/settings"
	"go-downloader/internal/store"
	"go-downloader/resources"
)

// deps are the world outside the app, so tests can replace it.
type deps struct {
	watchClipboard    func(func()) func()
	readClipboardText func() ([]byte, error)
	// Optional native acceptance observer; receives format names, never payloads.
	dropObserved func([]transfer.Format)
	dataDir      string
	defaultDir   string
	version      string
	secrets      store.SecretStore

	now              func() time.Time
	async            func(func())
	notify           func(title, body string)
	canNotify        func() bool
	appActive        func() bool
	newTray          func() (trayHandle, error)
	trayUnavailable  string
	backgroundWindow func(*mygo.Window) backgroundWindow
	quit             func()

	pickFolder      func(parent *mygo.Window, title string) (string, error)
	pickTaskList    func(parent *mygo.Window) (string, error)
	saveTaskList    func(parent *mygo.Window) (string, error)
	saveDiagnostics func(parent *mygo.Window) (string, error)
	openPath        func(path string) error
	reveal          func(path string)

	detectSystem func(ctx context.Context) (*proxy.System, error)
	testProxy    func(ctx context.Context, cfg proxy.Config, sys *proxy.System, target string) (time.Duration, error)
	applyTheme   func(settings.Theme)
}

// realDeps wires the app to MyGo and the operating system.
func realDeps() deps {
	version := appinfo.NormalizeVersion(mygo.App.Version())
	return deps{
		saveDiagnostics: func(parent *mygo.Window) (string, error) {
			return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: parent, Title: "导出本次诊断日志", DefaultPath: "diagnostics.json", Filters: []mygo.FileFilter{{Name: "JSON 日志", Extensions: []string{"json"}}}})
		},
		watchClipboard: mygo.Clipboard.OnChanged,
		readClipboardText: func() ([]byte, error) {
			return clipboardText(mygo.Clipboard.Formats, func() (transfer.Data, error) { return mygo.Clipboard.Read(transfer.Text) })
		},
		version:         version,
		trayUnavailable: trayPlatformReason(runtime.GOOS),
		newTray: func() (trayHandle, error) {
			return mygo.NewTray(mygo.TrayOptions{ID: "downloads", Icon: resources.IconPNG, ToolTip: appinfo.Name})
		},
		backgroundWindow: func(w *mygo.Window) backgroundWindow { return w },
		quit:             mygo.App.Quit,
		secrets:          appSecrets(),
		now:              time.Now,
		async:            func(fn func()) { go fn() },
		notify: func(title, body string) {
			if !mygo.NotificationsSupported() {
				return
			}
			_ = mygo.NewNotification(mygo.NotificationOptions{
				ID:    "download-complete",
				Title: title,
				Body:  body,
			}).Show()
		},
		canNotify: func() bool { return mygo.NotificationsSupported() },
		// App.IsActive is new in MyGo 0.7: whether the app is the one in
		// front, which is what decides if a completion needs a notification.
		appActive: func() bool { return mygo.App.IsActive() },
		pickFolder: func(parent *mygo.Window, title string) (string, error) {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
				Parent:            parent,
				Title:             title,
				Directory:         true,
				CreateDirectories: true,
				ButtonLabel:       "选择",
			})
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		},
		openPath: func(path string) error { return mygo.Shell.OpenPath(path) },
		pickTaskList: func(parent *mygo.Window) (string, error) {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: "导入 TXT 链接", Filters: []mygo.FileFilter{{Name: "文本文件", Extensions: []string{"txt"}}}})
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		},
		saveTaskList: func(parent *mygo.Window) (string, error) {
			return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: parent, Title: "导出脱敏任务清单", DefaultPath: "downloads.json", Filters: []mygo.FileFilter{{Name: "JSON 清单", Extensions: []string{"json"}}}})
		},
		reveal: func(path string) { mygo.Shell.ShowItemInFolder(path) },
		detectSystem: func(ctx context.Context) (*proxy.System, error) {
			return proxy.Detect(ctx)
		},
		testProxy: func(ctx context.Context, cfg proxy.Config, sys *proxy.System, target string) (time.Duration, error) {
			return proxy.Check(ctx, cfg, sys, target, version)
		},
		applyTheme: func(theme settings.Theme) {
			mygo.Theme.SetSource(themeSource(theme))
		},
	}
}

// mygoSecrets adapts MyGo's credential store to the store's interface.
type mygoSecrets struct{ service string }

func appSecrets() mygoSecrets {
	// Continue using the same credential namespace when reusing legacy data.
	root, err := mygo.App.Path(mygo.PathAppData)
	if err != nil {
		return mygoSecrets{}
	}
	dir := legacyDataDir(root, mygo.App.Name())
	return mygoSecrets{service: legacySecretService(filepath.Base(dir))}
}

func (s mygoSecrets) Get(key string) (string, error) {
	value, err := mygo.Secrets.Service(s.service).Get(key)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

func (s mygoSecrets) Set(key, value string) error {
	return mygo.Secrets.Service(s.service).Set(key, []byte(value))
}

func (s mygoSecrets) Delete(key string) error {
	return mygo.Secrets.Service(s.service).Delete(key)
}
