//go:build dropcheck

package app

import (
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"go-downloader/internal/proxy"
	"go-downloader/internal/settings"
	"go-downloader/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Isolated native acceptance harness, never built into the shipped application:
// go test -tags dropcheck -c -o .mygo/drop-check.exe ./internal/app
func TestMain(_ *testing.M) {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	dir := filepath.Join(filepath.Dir(exe), "drop-native-data")
	mygo.App.SetName("Downloader Drag Drop Check")
	mygo.App.SetPath(mygo.PathUserData, dir)
	mygo.App.SetPath(mygo.PathLogs, filepath.Join(dir, "logs"))
	mygo.App.SetLanguages("zh-Hans", "en")
	mygo.App.SetLocale("zh-Hans")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>本地拖放验收</title><body style="font:24px sans-serif;padding:40px"><h1>下载器拖放验收</h1><p><a href="/from-browser.bin">拖动这个本地下载链接</a></p><p><a href="ftp://127.0.0.1/unsupported">不支持的 FTP 链接</a></p><p>仅用于隔离测试，无外部网络请求。</p></body></html>`)
			return
		}
		fmt.Fprint(w, "local-drop-test-payload")
	}))
	defer server.Close()
	d := realDeps()
	d.dataDir = dir
	d.defaultDir = filepath.Join(dir, "downloads")
	d.secrets = nil
	if err := os.MkdirAll(d.defaultDir, 0700); err != nil {
		panic(err)
	}
	prefs := settings.Default(d.defaultDir)
	prefs.Proxy.Mode = proxy.ModeNone
	prefs.AutoStart = false
	s := &store.Store{Dir: dir}
	if err := s.SaveSettings(prefs); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "browser-url.txt"), []byte(server.URL), 0600); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "drop-links.txt"), []byte(server.URL+"/from-txt.bin\n"+server.URL+"/from-txt.bin\n"), 0600); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unsupported.url"), []byte("[InternetShortcut]\nURL="+server.URL), 0600); err != nil {
		panic(err)
	}
	formats := make(chan []transfer.Format, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f, err := os.OpenFile(filepath.Join(dir, "formats.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return
		}
		defer f.Close()
		for event := range formats {
			fmt.Fprintln(f, event)
		}
	}()
	d.dropObserved = func(f []transfer.Format) {
		select {
		case formats <- f:
		default:
		}
	}
	a := newApp(d)
	a.load()
	defer a.shutdown()
	mygo.App.WhenReady(func() { a.start(); a.win.SetTitle("拖放验收 · 隔离数据") })
	mygo.App.OnBeforeQuit(a.beforeQuit)
	mygo.App.OnQuitCanceled(a.quitCanceled)
	mygo.App.OnQuit(a.shutdown)
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	close(formats)
	<-done
}
