package app

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"go-downloader/internal/download"
	"go-downloader/internal/proxy"
	"go-downloader/internal/settings"
	"go-downloader/internal/tasklist"
)

// TestSnapshots renders the interface to PNG files for visual review:
//
//	DOWNLOADER_SNAPSHOT=1 go test -run TestSnapshots ./internal/app
func TestSnapshots(t *testing.T) {
	if os.Getenv("DOWNLOADER_SNAPSHOT") == "" {
		t.Skip("set DOWNLOADER_SNAPSHOT=1 to write the snapshots")
	}
	dir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(name string, tt *ui.Tester) {
		t.Helper()
		img := tt.Image()
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}

	engine := &fakeEngine{}
	engine.items = sampleItems()
	a := newTestApp(t, engine)
	a.s.DownloadDir = filepath.FromSlash("C:/Users/Ada/Downloads")
	a.deps.dataDir = filepath.FromSlash("C:/Users/Ada/AppData/Roaming/Downloader")
	a.system = &proxy.System{
		Source: "Windows 系统代理",
		HTTP:   &proxy.Endpoint{Kind: proxy.KindHTTP, Host: "127.0.0.1", Port: 7890},
		HTTPS:  &proxy.Endpoint{Kind: proxy.KindHTTP, Host: "127.0.0.1", Port: 7890},
	}
	a.rebuildClient()
	tt := testerFor(a)
	tt.Frame()
	save("light-list.png", tt)

	// The enabled primary action and a chosen row, for reviewing the accent.
	a.input = "https://example.com/file.zip"
	a.selection.Add("d1")
	tt.Frame()
	save("light-list-actions.png", tt)
	a.input = ""
	a.selection.Clear()
	tt.Frame()

	tt.SetDark(true)
	tt.Frame()
	save("dark-list.png", tt)

	// Review compact caption controls at rest and under the pointer in both
	// themes, including their actual size at 125% display scaling.
	caption := ui.NewTester(func(c *ui.Context) {
		a.palette = paletteFor(c.Theme().Dark)
		c.SetTheme(a.palette.theme())
		a.header(c, a.summary)
	}, 1240, titleBarHeight)
	caption.SetLocale("zh-Hans")
	caption.SetScale(1.25)
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		caption.SetDark(dark)
		for _, button := range []struct{ label, name string }{
			{"最大化窗口", "maximize"}, {"关闭窗口", "close"},
		} {
			r, ok := caption.Find(button.label)
			if !ok {
				t.Fatalf("missing caption control %s", button.label)
			}
			caption.Move(r.X+r.W/2, r.Y+r.H/2)
			save(name+"-titlebar-"+button.name+"-hover.png", caption)
		}
	}

	a.input = "https://example.com/file.zip"
	a.selection.Add("d1")
	tt.Frame()
	save("dark-list-actions.png", tt)
	a.input = ""
	a.selection.Clear()
	tt.Frame()

	tt.SetDark(false)
	a.setView(settings.ViewCards)
	tt.Frame()
	save("light-cards.png", tt)

	tt.SetDark(true)
	tt.Frame()
	save("dark-cards.png", tt)

	tt.SetDark(false)
	a.setView(settings.ViewList)
	a.settingsTab = 0
	a.settingsOpen = true
	tt.Frame()
	save("light-settings-general.png", tt)

	a.settingsTab = 1
	a.proxyTest = proxyTest{ok: true, result: "连接成功 · 32 ms"}
	tt.Frame()
	save("light-settings-proxy.png", tt)

	// A custom proxy, as the manual form lays it out.
	a.proxyDraft.Mode = proxy.ModeManual
	a.proxyDraft.Kind = proxy.KindSOCKS5
	a.proxyDraft.Host = "127.0.0.1"
	a.proxyPort = 1080
	a.proxyKindLabel = proxyKindLabel(proxy.KindSOCKS5)
	a.proxyModeIdx = 2
	tt.Frame()
	save("light-settings-proxy-manual.png", tt)

	a.settingsTab = 2
	tt.Frame()
	save("light-settings-appearance.png", tt)

	a.settingsTab = 3
	tt.Frame()
	save("light-settings-about.png", tt)

	a.settingsTab = 1
	tt.SetDark(true)
	tt.Frame()
	save("dark-settings-proxy.png", tt)
	for _, tab := range []struct {
		index int
		name  string
	}{
		{0, "general"}, {2, "appearance"}, {3, "about"},
	} {
		a.settingsTab = tab.index
		tt.Frame()
		save("dark-settings-"+tab.name+".png", tt)
	}

	tt.SetDark(false)
	empty := newTestApp(t, &fakeEngine{})
	empty.s.DownloadDir = a.s.DownloadDir
	tts := testerFor(empty)
	tts.Frame()
	save("light-empty.png", tts)

	tts.SetDark(true)
	tts.Frame()
	save("dark-empty.png", tts)

	// Selection must remain readable after the sidebar takes keyboard focus.
	focused := newTestApp(t, &fakeEngine{items: sampleItems()})
	focused.s.DownloadDir = a.s.DownloadDir
	ft := testerFor(focused)
	if err := ft.Click("下载完成"); err != nil {
		t.Fatal(err)
	}
	ft.Frame()
	save("light-sidebar-selected.png", ft)
	ft.SetDark(true)
	ft.Frame()
	save("dark-sidebar-selected.png", ft)

	// The supported minimum window size must keep actions reachable and
	// make long settings pages scroll rather than overflow the dialog.
	a.settingsOpen = false
	a.selection.Clear()
	tt.SetSize(960, 640)
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		tt.SetDark(dark)
		for _, view := range viewValues {
			a.setView(view)
			tt.Frame()
			save(name+"-compact-"+string(view)+".png", tt)
		}
		a.settingsOpen = true
		a.settingsTab = 1
		tt.Frame()
		save(name+"-compact-settings.png", tt)
		a.settingsOpen = false
		a.requestRemove([]string{"d1", "d3"})
		tt.Frame()
		save(name+"-confirm.png", tt)
		a.confirm.open = false
	}

	// Review the shared app icon in the header and About page at 125% on
	// the minimum supported window, where its bitmap must stay crisp.
	tt.SetScale(1.25)
	a.setView(settings.ViewList)
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		tt.SetDark(dark)
		tt.Frame()
		save(name+"-compact-125-list.png", tt)
		a.settingsOpen = true
		a.settingsTab = 3
		tt.Frame()
		save(name+"-compact-125-about.png", tt)
		a.settingsOpen = false
	}

	// First-phase dialogs and the bandwidth setting at minimum size and 125%.
	features := newTestApp(t, &fakeEngine{items: sampleItems()})
	features.s.DownloadDir = a.s.DownloadDir
	features.deps.dataDir = a.deps.dataDir
	featureTester := testerFor(features)
	featureTester.SetSize(960, 640)
	featureTester.SetScale(1.25)
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		featureTester.SetDark(dark)
		for _, state := range []string{"empty", "paused", "completed", "peak", "gap"} {
			f := features.engine.(*fakeEngine)
			original := f.items[0]
			f.items[0].State = download.StateDownloading
			if state == "paused" {
				f.items[0].State = download.StatePaused
				f.items[0].Speed = 0
			}
			if state == "completed" {
				f.items[0].State = download.StateCompleted
				f.items[0].Speed = 0
				f.items[0].Done = f.items[0].Total
				f.items[0].FinishedAt = testNow.Add(-30 * time.Second)
			}
			f.histories = map[string][]download.SpeedSample{original.ID: historyFixture(state)}
			features.openDetails(original.ID)
			featureTester.Frame()
			featureTester.Wheel(600, 370, 0, -5000)
			featureTester.Advance(500 * time.Millisecond)
			featureTester.Wheel(600, 370, 0, 350)
			featureTester.Advance(500 * time.Millisecond)
			save(name+"-compact-125-history-"+state+".png", featureTester)
			features.detailsOpen = false
			f.items[0] = original
			f.histories = nil
		}
		features.openSettings(3)
		featureTester.Frame()
		save(name+"-compact-125-diagnostics.png", featureTester)
		features.diagnosticExport.busy = true
		features.diagnosticExport.message = "正在导出…"
		featureTester.Frame()
		save(name+"-compact-125-diagnostics-busy.png", featureTester)
		features.diagnosticExport = diagnosticExportState{}
		features.settingsOpen = false
		features.s.ClipboardPrompts = true
		features.clipboard.pending = []string{"https://user:secret@example.com/releases/archive.zip?token=private"}
		featureTester.Frame()
		save(name+"-compact-125-clipboard-prompt.png", featureTester)
		features.clipboard.pending = nil
		features.openSettings(0)
		featureTester.Frame()
		featureTester.Wheel(600, 400, 0, 1200)
		featureTester.Advance(500 * time.Millisecond)
		save(name+"-compact-125-clipboard-settings.png", featureTester)
		features.settingsOpen = false
		features.s.ClipboardPrompts = false
		hoverTester := ui.NewTester(func(c *ui.Context) {
			features.palette = paletteFor(c.Theme().Dark)
			c.SetTheme(features.palette.theme())
			features.drawAddCard(c, func(ui.Element) bool { return true })
		}, 960, 240)
		hoverTester.SetScale(1.25)
		hoverTester.SetDark(dark)
		hoverTester.Frame()
		save(name+"-compact-125-drop-hover.png", hoverTester)
		features.beginDrop(testURIData("https://example.com/a.zip\nhttps://example.com/b.zip"))
		featureTester.Frame()
		save(name+"-compact-125-drop-preview.png", featureTester)
		features.closeTaskList()
		features.s.DirectoryRules = []settings.DirectoryRule{
			{ID: "archives", Enabled: true, Kind: "extension", Pattern: "zip", Dir: filepath.Join(features.s.DownloadDir, "压缩包")},
			{ID: "domain", Enabled: true, Kind: "domain", Pattern: "*.example.com", Dir: filepath.Join(features.s.DownloadDir, "项目资料")},
		}
		features.openRules()
		features.ruleEditor.previewURL = "https://files.example.com/archive.zip"
		featureTester.Frame()
		save(name+"-compact-125-rules.png", featureTester)
		features.ruleEditor.open = false
		queueItem := &features.engine.(*fakeEngine).items[1]
		queueItem.State = download.StateQueued
		queueItem.QueuePosition, queueItem.QueueLength = 2, 3
		features.openDetails(queueItem.ID)
		featureTester.Frame()
		save(name+"-compact-125-queue.png", featureTester)
		features.detailsOpen = false
		features.openSchedule(download.Item{ID: "planned", Schedule: download.Schedule{Enabled: true, StartAt: time.Date(2027, 1, 2, 22, 0, 0, 0, time.Local), DailyEnabled: true, DailyStartMinute: 1320, DailyEndMinute: 360}})
		featureTester.Frame()
		save(name+"-compact-125-schedule.png", featureTester)
		features.scheduleEditor.open = false
		features.openAdvanced()
		features.advanced.url = "https://example.com/download/archive.zip"
		features.advanced.fileName = "项目资料.zip"
		features.advanced.rateKiB = "128"
		featureTester.Frame()
		save(name+"-compact-125-advanced.png", featureTester)
		featureTester.Wheel(600, 370, 0, 420)
		featureTester.Advance(500 * time.Millisecond)
		save(name+"-compact-125-advanced-headers.png", featureTester)
		features.advanced.schedule = newScheduleDraft(download.Schedule{Enabled: true, DailyEnabled: true, DailyStartMinute: 1320, DailyEndMinute: 360})
		featureTester.Wheel(600, 370, 0, 1000)
		featureTester.Advance(500 * time.Millisecond)
		save(name+"-compact-125-advanced-schedule.png", featureTester)
		features.clearAdvanced()
		features.openDetails("d1")
		featureTester.Frame()
		save(name+"-compact-125-details.png", featureTester)
		features.detailsRate = "-1"
		features.applyTaskRateLimit()
		featureTester.Frame()
		save(name+"-compact-125-details-rate-error.png", featureTester)
		features.openDetails("d3")
		featureTester.Frame()
		featureTester.Wheel(600, 370, 0, 1800)
		featureTester.Advance(500 * time.Millisecond)
		save(name+"-compact-125-details-error.png", featureTester)
		features.detailsOpen = false
		for _, state := range []download.VerificationState{download.VerificationChecking, download.VerificationMatched, download.VerificationMismatched, download.VerificationPending} {
			for i := range features.engine.(*fakeEngine).items {
				it := &features.engine.(*fakeEngine).items[i]
				if it.ID != "d4" {
					continue
				}
				it.ExpectedSHA256 = strings.Repeat("ab", 32)
				it.VerificationState = state
				it.VerificationDone = 4 * 1024 * 1024
				it.VerificationTotal = 12 * 1024 * 1024
				it.LastVerification = download.VerificationResult{State: download.VerificationMatched, ExpectedSHA256: it.ExpectedSHA256, ActualSHA256: it.ExpectedSHA256, CheckedAt: testNow, Size: it.Total}
				if state == download.VerificationMismatched {
					it.LastVerification.State = state
					it.LastVerification.ActualSHA256 = strings.Repeat("cd", 32)
				}
			}
			features.openDetails("d4")
			featureTester.Frame()
			save(name+"-compact-125-verification-"+string(state)+".png", featureTester)
		}
		features.detailsOpen = false
		features.openSettings(0)
		featureTester.Frame()
		save(name+"-compact-125-rate-limit.png", featureTester)
		featureTester.Wheel(600, 400, 0, 600)
		featureTester.Advance(500 * time.Millisecond)
		features.tray.reason = trayPlatformReason("windows")
		featureTester.Frame()
		save(name+"-compact-125-tray-unavailable.png", featureTester)
		features.tray.reason = ""
		features.tray.handle = &fakeTray{}
		features.s.CloseToTray = true
		featureTester.Frame()
		featureTester.Advance(300 * time.Millisecond)
		save(name+"-compact-125-tray-enabled.png", featureTester)
		features.tray.handle = nil
		features.s.CloseToTray = false
		features.settingsOpen = false
		features.openImport()
		var links strings.Builder
		for i := range 80 {
			fmt.Fprintf(&links, "https://example.com/releases/file-%02d.zip\n", i)
		}
		links.WriteString("https://example.com/releases/file-00.zip\n无效文本\n")
		features.taskList.preview, _ = tasklist.Parse(context.Background(), []byte(links.String()), nil)
		featureTester.Frame()
		save(name+"-compact-125-import.png", featureTester)
		features.closeTaskList()
		features.openExport()
		featureTester.Frame()
		save(name+"-compact-125-export.png", featureTester)
		features.closeTaskList()
		features.openSource("d3")
		features.source.url = "https://example.com/driver-setup.exe?signature=renewed"
		features.source.preview = download.SourcePreview{URL: "https://example.com/driver-setup.exe", Reason: "无法证明为同一资源，将从头下载"}
		featureTester.Frame()
		save(name+"-compact-125-source-reset.png", featureTester)
		features.source.preview.Reuse = true
		features.source.preview.Reason = "已有前缀已逐字节验证，可保留进度继续下载"
		featureTester.Frame()
		save(name+"-compact-125-source-reuse.png", featureTester)
		features.source.saved = true
		features.source.url = ""
		featureTester.Frame()
		save(name+"-compact-125-source-saved.png", featureTester)
		features.closeSource()
		features.engine.(*fakeEngine).items[0].ConnectionLimit = 4
		features.engine.(*fakeEngine).items[0].TransferMode = "segments"
		features.engine.(*fakeEngine).items[0].ActiveConnections = 2
		features.engine.(*fakeEngine).items[0].WaitingConnections = 2
		features.engine.(*fakeEngine).items[0].FallbackReason = ""
		features.openDetails("d1")
		featureTester.Frame()
		save(name+"-compact-125-connections.png", featureTester)
		featureTester.Wheel(600, 370, 0, 230)
		featureTester.Advance(500 * time.Millisecond)
		features.detailsConnections = "9"
		features.applyTaskConnections()
		features.engine.(*fakeEngine).items[0].TransferMode = "single"
		features.engine.(*fakeEngine).items[0].ActiveConnections = 1
		features.engine.(*fakeEngine).items[0].WaitingConnections = 0
		features.engine.(*fakeEngine).items[0].FallbackReason = "服务器未提供可靠的 206、强 ETag 和未压缩表示"
		featureTester.Frame()
		save(name+"-compact-125-connections-error.png", featureTester)
		features.detailsOpen = false
		features.openSettings(0)
		featureTester.Frame()
		save(name+"-compact-125-connection-settings.png", featureTester)
		features.settingsOpen = false
	}
}

// sampleItems is a mix of states for the snapshots.
func sampleItems() []download.Item {
	mk := func(id, name string, state download.State, total, done int64) download.Item {
		it := download.Item{
			ID:         id,
			URL:        "https://mirrors.example.com/releases/" + name,
			DisplayURL: "https://mirrors.example.com/releases/" + name,
			FileName:   name,
			Path:       filepath.Join("C:", "Users", "Ada", "Downloads", name),
			State:      state,
			Total:      total,
			Done:       done,
			CreatedAt:  testNow.Add(-time.Duration(len(id)+1) * time.Minute),
		}
		switch state {
		case download.StateDownloading:
			it.Speed = 6.4 * 1024 * 1024
			it.ETA = 4*time.Minute + 12*time.Second
			it.Resumable = true
		case download.StateCompleted:
			it.FinishedAt = testNow.Add(-9 * time.Minute)
			it.Done = total
		case download.StateFailed:
			it.Error = "网络错误：连接被重置，点击重试"
		}
		return it
	}
	return []download.Item{
		mk("d1", "ubuntu-24.04.1-desktop-amd64.iso", download.StateDownloading, 5200*1024*1024, 1400*1024*1024),
		mk("d2", "设计稿-final-v3.zip", download.StateDownloading, 268*1024*1024, 189*1024*1024),
		mk("d3", "driver-setup.exe", download.StateFailed, 96*1024*1024, 32*1024*1024),
		mk("d4", "报告-2026-Q4.pdf", download.StateCompleted, 12*1024*1024, 12*1024*1024),
		mk("d5", "音乐合集.flac", download.StatePaused, 640*1024*1024, 210*1024*1024),
		mk("d6", "notes-archive.tar.gz", download.StateQueued, 48*1024*1024, 0),
	}
}
