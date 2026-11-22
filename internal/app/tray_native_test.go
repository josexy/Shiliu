//go:build nativecheck

package app

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"go-downloader/internal/download"
	"go-downloader/internal/proxy"
	"go-downloader/internal/settings"
	"go-downloader/internal/store"
)

// The explicit nativecheck tag builds an interactive acceptance executable.
// It never runs in ordinary tests and never uses the user's data or credentials:
// go test -tags nativecheck -c -o .mygo/tray-check.exe ./internal/app
func TestMain(_ *testing.M) {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	dir := filepath.Join(filepath.Dir(exe), "tray-native-data")
	mygo.App.SetName("Downloader Native Check")
	mygo.App.SetPath(mygo.PathUserData, dir)
	mygo.App.SetPath(mygo.PathLogs, filepath.Join(dir, "logs"))
	var server *httptest.Server
	err = run(func() deps {
		d := realDeps()
		d.dataDir = dir
		d.defaultDir = filepath.Join(dir, "downloads")
		d.secrets = nil
		payload := bytes.Repeat([]byte("native-local-download\n"), 1024*1024)
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("ETag", `"native-check-v1"`)
			if r.URL.Path == "/notification.bin" {
				w.Header().Set("Content-Length", "4")
				fmt.Fprint(w, "n")
				w.(http.Flusher).Flush()
				for {
					if _, err := os.Stat(filepath.Join(dir, "complete-notification")); err == nil {
						fmt.Fprint(w, "ote")
						return
					}
					select {
					case <-r.Context().Done():
						return
					case <-time.After(50 * time.Millisecond):
					}
				}
			}
			http.ServeContent(w, r, "background.bin", time.Unix(1700000000, 0), bytes.NewReader(payload))
		}))
		s := &store.Store{Dir: dir}
		prefs := settings.Default(d.defaultDir)
		prefs.Proxy.Mode = proxy.ModeNone
		prefs.CloseToTray = true
		prefs.AutoStart = false
		prefs.RateLimit = 128 * 1024
		if err := s.SaveSettings(prefs); err != nil {
			panic(err)
		}
		records, _ := s.LoadTasks()
		if len(records) == 0 {
			for _, name := range []string{"background.bin", "notification.bin"} {
				records = append(records, download.Record{ID: name, FileName: name, Path: filepath.Join(d.defaultDir, name), State: download.StatePaused, Total: -1, CreatedAt: time.Now()})
			}
		}
		for i := range records {
			records[i].URL = server.URL + "/" + records[i].FileName
		}
		if err := s.SaveTasks(records); err != nil {
			panic(err)
		}
		return d
	})
	if server != nil {
		server.Close()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
