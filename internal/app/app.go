package app

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"go-downloader/internal/diagnostics"
	"go-downloader/internal/download"
	"go-downloader/internal/proxy"
	"go-downloader/internal/settings"
	"go-downloader/internal/store"
)

// Download filters shown in the sidebar.
const (
	filterAll       = "all"
	filterActive    = "active"
	filterPaused    = "paused"
	filterCompleted = "completed"
	filterFailed    = "failed"
)

// proxyTestURL is small and neutral, for the settings' connection test.
const proxyTestURL = "https://example.com/"

// downloader is the part of the download engine the view uses, so tests can
// replace it with a fake.
type downloader interface {
	Add(raw string) (added int, skipped int, invalid []string)
	AddContext(context.Context, string) (added int, skipped int, err error)
	AddTask(download.AddOptions) (string, error)
	BeginSourceUpdate(string, download.SourceOptions) (*download.SourceDraft, error)
	ProbeSource(context.Context, *download.SourceDraft) (download.SourcePreview, error)
	CommitSource(*download.SourceDraft) error
	Pause(id string)
	Resume(id string)
	Retry(id string)
	MoveQueue(string, download.QueueMove) error
	SetSchedule(string, download.Schedule) error
	SetDirectoryRules([]settings.DirectoryRule) error
	StartScheduling()
	Remove(id string, deleteFile bool)
	RemoveMany(ids []string, deleteFiles bool)
	PauseAll()
	ResumeAll()
	ClearCompleted()
	SetDir(dir string)
	SetMaxConcurrent(n int)
	SetMaxConnections(int) error
	SetDefaultConnections(int) error
	SetTaskConnections(string, int) error
	SetAutoStart(on bool)
	SetRateLimit(bytesPerSecond int64)
	SetTaskRateLimit(string, int64) error
	SetExpectedSHA256(string, string) error
	Verify(string) error
	CancelVerification(string)
	RefreshFiles()
	Snapshot() ([]download.Item, download.Summary)
	History(string) []download.SpeedSample
	Records() []download.Record
	AcknowledgeSources([]download.Record) error
	Restore([]download.Record)
	SetOnChange(func())
	SetOnComplete(func(download.Item))
	Shutdown()
}

// app is the window's state and its view's data.
type app struct {
	diagnostics      *diagnostics.Buffer
	diagnosticExport diagnosticExportState
	deps             deps
	store            *store.Store
	engine           downloader
	win              *mygo.Window

	s       settings.Settings
	palette palette
	dark    bool

	// UI values bound to widgets persist here, out of the build pass.
	themeIdx           int
	viewIdx            int
	sortLabel          string
	maxLabel           string
	globalConnections  string
	defaultConnections string
	connectionsErr     string
	rateKiB            float64
	settingsTab        int
	proxyModeIdx       int
	proxyKindLabel     string
	proxyPort          float64
	proxyDraft         proxy.Config
	passwordDirty      bool
	proxyErr           string

	// View state.
	input                 string
	clipboard             clipboardState
	addBusy               bool
	dropSeen              bool
	dropBatchID           uint64
	quickInput            ui.Handle
	restoreQuickInput     bool
	focusQuickInput       bool
	ruleEditor            ruleEditorState
	inputErr              string
	query                 string
	filter                string
	visible               []download.Item
	summary               download.Summary
	railOn                bool
	railValue             float64
	selection             ui.Selection[string]
	list                  ui.ListState
	grid                  ui.GridState
	focusSearch           bool
	settingsOpen          bool
	settingsWasOpen       bool
	proxyTest             proxyTest
	system                *proxy.System
	systemErr             string
	systemRefreshing      bool
	confirm               confirmState
	advanced              advancedState
	advancedWasOpen       bool
	taskList              taskListState
	source                sourceState
	taskListJobs          sync.WaitGroup
	stopping              bool
	tray                  trayState
	lifetime              context.Context
	cancelLifetime        context.CancelFunc
	backgroundJobs        sync.WaitGroup
	detailsID             string
	scheduleEditor        scheduleEditorState
	detailsOpen           bool
	detailsRate           string
	detailsRateErr        string
	detailsConnections    string
	detailsConnectionsErr string
	detailsSHA            string
	detailsSHAErr         string
	toasts                []string
	now                   time.Time
	canNotify             bool

	client    atomic.Pointer[clientHolder]
	dirty     atomic.Bool
	shrunk    sync.Once
	saverCh   chan struct{}
	saverWG   sync.WaitGroup
	persistMu sync.Mutex
}

type clientHolder struct {
	client *http.Client
	err    error
}

type proxyTest struct {
	running bool
	ok      bool
	result  string
}

// confirmState is the modal question a destructive action asks.
type confirmState struct {
	open         bool
	title        string
	message      string
	confirmLabel string
	deleteOption bool
	deleteFiles  bool
	onConfirm    func(deleteFiles bool)
}

func newApp(d deps) *app {
	lifetime, cancel := context.WithCancel(context.Background())
	a := &app{
		diagnostics: &diagnostics.Buffer{},
		lifetime:    lifetime, cancelLifetime: cancel,
		deps:      d,
		filter:    filterAll,
		proxyPort: 7890,
	}
	if a.deps.now == nil {
		a.deps.now = time.Now
	}
	if a.deps.async == nil {
		a.deps.async = func(fn func()) { go fn() }
	}
	return a
}

// load reads the settings and the saved downloads before the window opens.
func (a *app) load() {
	if a.deps.dataDir == "" {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil && dir != "" {
			a.deps.dataDir = dir
		}
	}
	if a.deps.dataDir == "" {
		if config, err := os.UserConfigDir(); err == nil {
			a.deps.dataDir = filepath.Join(config, "Downloader")
		}
	}
	if a.deps.dataDir == "" {
		a.deps.dataDir = os.TempDir()
	}
	if err := os.MkdirAll(a.deps.dataDir, 0o755); err != nil {
		a.toast("无法创建应用数据目录：" + err.Error())
	}
	if a.deps.defaultDir == "" {
		a.deps.defaultDir = defaultDownloadDir()
	}
	a.store = &store.Store{Dir: a.deps.dataDir, Secrets: a.deps.secrets}
	s, err := a.store.LoadSettings(a.deps.defaultDir)
	if err != nil {
		a.toast(err.Error())
	}
	a.s = s
	a.syncFields()
	records, err := a.store.LoadTasks()
	if err != nil {
		a.toast(err.Error())
	}
	a.engine = download.New(download.Options{
		Diagnostics:     a.diagnostics,
		DeferScheduling: true,
		DirectoryRules:  a.s.DirectoryRules,
		Dir:             a.s.DownloadDir,
		AppVersion:      a.deps.version,
		MaxConcurrent:   a.s.MaxConcurrent,
		MaxConnections:  a.s.MaxConnections, DefaultConnections: a.s.TaskConnections,
		AutoStart:     a.s.AutoStart,
		RateLimit:     a.s.RateLimit,
		ClientFactory: a.clientSnapshot,
		OnChange:      a.onEngineChange,
		OnComplete:    a.onEngineComplete,
		StallTimeout:  90 * time.Second,
	})
	a.engine.Restore(records)
	a.rebuildClient()
}

// start runs when the app is ready: it loads the proxy password, reads the
// system proxy, opens the window and starts saving.
func (a *app) start() {
	if a.store != nil && a.deps.secrets != nil {
		if password := a.store.LoadProxyPassword(); password != "" {
			a.s.Proxy.Password = password
			a.proxyDraft.Password = password
		}
	}
	a.refreshSystemSync()
	a.rebuildClient()
	if a.deps.canNotify != nil {
		a.canNotify = a.deps.canNotify()
	}
	a.deps.applyTheme(a.s.Theme)
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:             "下载器",
		Width:             1200,
		Height:            800,
		MinWidth:          960,
		MinHeight:         640,
		StateKey:          "main",
		TitleBarStyle:     mygo.TitleBarHidden,
		TitleBarHeight:    titleBarHeight,
		HideWindowButtons: true,
		BackgroundColor:   "light-dark(#F4F6FA, #151B26)",
		Content:           ui.View(a.view),
	})
	if a.deps.backgroundWindow != nil {
		a.bindBackgroundWindow(a.deps.backgroundWindow(a.win))
	} else {
		a.bindBackgroundWindow(a.win)
	}
	a.initTray()
	a.engine.SetOnChange(a.onEngineChange)
	a.engine.SetOnComplete(a.onEngineComplete)
	a.startSaver()
	a.engine.StartScheduling()
	a.syncClipboard()
}

// shutdown pauses the downloads and saves everything before the app quits.
func (a *app) shutdown() {
	a.shrunk.Do(func() {
		a.stopping = true
		a.cancelDiagnosticExport()
		a.stopClipboard()
		if a.cancelLifetime != nil {
			a.cancelLifetime()
		}
		a.closeTaskList()
		a.closeSource()
		a.taskListJobs.Wait()
		a.backgroundJobs.Wait()
		if a.saverCh != nil {
			close(a.saverCh)
			a.saverWG.Wait()
		}
		if a.engine != nil {
			a.engine.Shutdown()
		}
		a.stopTray()
		if a.store == nil {
			return
		}
		if a.engine != nil {
			if err := a.persistTasks(); err != nil {
				log.Println(err)
			}
		}
		a.s.Proxy.Password = a.proxyDraft.Password
		if err := a.store.SaveSettings(a.s); err != nil {
			log.Println(err)
		}
		if err := a.store.SaveProxyPassword(a.s.Proxy.Password); err != nil {
			log.Println(err)
		}
	})
}

// startSaver writes the download records every couple of seconds when they
// changed.
func (a *app) startSaver() {
	a.saverCh = make(chan struct{})
	refreshTray := a.tray.handle != nil
	a.saverWG.Add(1)
	go func() {
		defer a.saverWG.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-a.saverCh:
				return
			case <-ticker.C:
				if refreshTray {
					a.dispatch(a.refreshTray)
				}
				a.engine.RefreshFiles()
				if a.dirty.Swap(false) {
					if err := a.persistTasks(); err != nil {
						log.Println(err)
					}
				}
			}
		}
	}()
}

// Take the snapshot inside the persistence lock so an earlier saver cannot
// overwrite a source revision after its journal has been acknowledged.
func (a *app) persistTasks() error {
	a.persistMu.Lock()
	defer a.persistMu.Unlock()
	records := a.engine.Records()
	if err := a.store.SaveTasks(records); err != nil {
		return err
	}
	return a.engine.AcknowledgeSources(records)
}

// dispatch runs fn where the view reads its state.
func (a *app) dispatch(fn func()) {
	if a.win != nil {
		a.win.Update(fn)
		return
	}
	fn()
}

// onEngineChange is called from worker goroutines; it only asks for a frame.
func (a *app) onEngineChange() {
	a.dirty.Store(true)
	if a.win != nil {
		a.win.Invalidate()
	}
}

// onEngineComplete runs on a worker goroutine and hands the completion to the
// main thread.
func (a *app) onEngineComplete(it download.Item) {
	a.dispatch(func() {
		title, body := "下载完成", it.FileName
		message := "已下载完成：" + it.FileName
		if it.ExpectedSHA256 != "" {
			title = "传输完成"
			body += "（SHA-256 校验结果请查看任务详情）"
			message = title + "：" + body
		}
		a.toast(message)
		if a.s.NotifyOnComplete && a.canNotify && a.deps.notify != nil && !a.appActive() {
			a.deps.notify(title, body)
		}
	})
}

func (a *app) appActive() bool {
	if a.deps.appActive != nil {
		return a.deps.appActive()
	}
	if a.win != nil {
		return a.win.IsFocused()
	}
	return true
}

// clientSnapshot returns the client the engine uses now.
func (a *app) clientSnapshot() (*http.Client, error) {
	if h := a.client.Load(); h != nil && h.client != nil {
		return h.client, h.err
	}
	client, err := proxy.NewClient(proxy.Config{Mode: proxy.ModeNone}, nil)
	if err != nil {
		return http.DefaultClient, err
	}
	a.client.CompareAndSwap(nil, &clientHolder{client: client})
	if h := a.client.Load(); h != nil && h.client != nil {
		return h.client, h.err
	}
	return client, nil
}

// rebuildClient prepares a client for the current proxy settings, keeping the
// previous one when the settings are incomplete.
func (a *app) rebuildClient() {
	cfg := a.s.Proxy
	cfg.Normalize()
	client, err := proxy.NewClientWithDiagnostics(cfg, a.system, a.diagnostics)
	if err != nil {
		a.diagnostics.Record(diagnostics.NewEvent(diagnostics.ProxyFailure, "", "", 0, 0, err))
		a.proxyErr = err.Error()
		return
	}
	a.proxyErr = ""
	old := a.client.Swap(&clientHolder{client: client, err: nil})
	if old != nil && old.client != nil && old.client != client {
		old.client.CloseIdleConnections()
	}
}

// refreshSystemSync reads the system proxy before the window opens.
func (a *app) refreshSystemSync() {
	if a.deps.detectSystem == nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.lifetime, 3*time.Second)
	defer cancel()
	sys, err := a.deps.detectSystem(ctx)
	a.system = sys
	if err != nil {
		a.systemErr = err.Error()
	} else {
		a.systemErr = ""
	}
}

// refreshSystemAsync reads the system proxy again, for the settings page.
func (a *app) refreshSystemAsync() {
	if a.stopping || a.systemRefreshing || a.deps.detectSystem == nil {
		return
	}
	a.systemRefreshing = true
	lifetime := a.lifetime
	a.backgroundJobs.Add(1)
	a.deps.async(func() {
		defer a.backgroundJobs.Done()
		ctx, cancel := context.WithTimeout(lifetime, 4*time.Second)
		defer cancel()
		sys, err := a.deps.detectSystem(ctx)
		if lifetime.Err() != nil {
			return
		}
		a.dispatch(func() {
			if a.stopping {
				return
			}
			a.systemRefreshing = false
			a.system = sys
			if err != nil {
				a.systemErr = err.Error()
				a.toast("读取系统代理失败：" + err.Error())
			} else {
				a.systemErr = ""
			}
			a.rebuildClient()
		})
	})
}

// defaultDownloadDir finds the system's Downloads folder.
func defaultDownloadDir() string {
	if dir, err := mygo.App.Path(mygo.PathDownloads); err == nil && dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Downloads")
	}
	return os.TempDir()
}

// computeVisible filters and sorts the downloads for the list or the grid.
// Its copied order is presentation only; the engine owns queue priority.
func (a *app) computeVisible(items []download.Item) []download.Item {
	out := make([]download.Item, 0, len(items))
	query := strings.ToLower(strings.TrimSpace(a.query))
	for _, it := range items {
		if !matchesFilter(it, a.filter) {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(it.FileName + " " + it.URL)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		out = append(out, it)
	}
	switch a.s.Sort {
	case settings.SortName:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].FileName) < strings.ToLower(out[j].FileName)
		})
	case settings.SortSize:
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Total != out[j].Total {
				return out[i].Total > out[j].Total
			}
			return out[i].FileName < out[j].FileName
		})
	case settings.SortStatus:
		sort.SliceStable(out, func(i, j int) bool {
			ri, rj := statusRank(out[i].State), statusRank(out[j].State)
			if ri != rj {
				return ri < rj
			}
			return out[i].CreatedAt.After(out[j].CreatedAt)
		})
	default: // newest
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		})
	}
	return out
}

func matchesFilter(it download.Item, filter string) bool {
	switch filter {
	case filterActive:
		return it.State.Active()
	case filterPaused:
		return it.State == download.StatePaused
	case filterCompleted:
		return it.State == download.StateCompleted
	case filterFailed:
		return it.State == download.StateFailed
	default:
		return true
	}
}

func statusRank(s download.State) int {
	switch s {
	case download.StateDownloading:
		return 0
	case download.StateConnecting:
		return 1
	case download.StateQueued:
		return 2
	case download.StatePaused:
		return 3
	case download.StateFailed:
		return 4
	case download.StateCompleted:
		return 5
	default:
		return 6
	}
}

// pruneSelection drops chosen IDs that the filter or the search hid.
func (a *app) pruneSelection() {
	if a.selection.Len() == 0 {
		return
	}
	visible := make(map[string]bool, len(a.visible))
	for _, it := range a.visible {
		visible[it.ID] = true
	}
	var stale []string
	for id := range a.selection.All() {
		if !visible[id] {
			stale = append(stale, id)
		}
	}
	for _, id := range stale {
		a.selection.Remove(id)
	}
}

// toast queues a message for the next frame.
func (a *app) toast(message string) {
	if message == "" {
		return
	}
	a.toasts = append(a.toasts, message)
}

// numberLabel formats a number for a select option.
func numberLabel(n int) string { return strconv.Itoa(n) }

func mustAtoi(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}
