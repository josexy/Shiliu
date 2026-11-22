package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Options configures a Manager. The zero value is not usable: set Dir.
type Options struct {
	// Dir is where new downloads are saved.
	Dir string
	// AppVersion identifies the app in requests (default dev).
	AppVersion string
	// MaxConcurrent is how many downloads run at once (default 3).
	MaxConcurrent int
	// Connections are request counts, independent of concurrent tasks.
	MaxConnections     int // default 16, maximum 64
	DefaultConnections int // new tasks: default 1, maximum 8
	// AutoStart starts downloads as they are added.
	AutoStart bool
	// RateLimit is the total bytes per second across all tasks (0: unlimited).
	RateLimit int64
	// ClientFactory returns the HTTP client for a new connection. It is
	// called on worker goroutines and may change with the proxy settings.
	ClientFactory func() (*http.Client, error)
	// OnChange is called from any goroutine when a download changed.
	OnChange func()
	// OnComplete is called from a worker goroutine when a file is done.
	OnComplete func(Item)
	// StallTimeout is how long a silent connection may take before it is
	// retried (default 90s).
	StallTimeout time.Duration
}

// Manager is the download queue. Its methods are safe from any goroutine.
type Manager struct {
	mu       sync.Mutex
	opts     Options
	dir      string
	max      int
	auto     bool
	stall    time.Duration
	tasks    []*task
	byID     map[string]*task
	reserved map[string]bool
	active   int
	closed   bool
	wg       sync.WaitGroup

	lastNotify         atomic.Int64
	bandwidth          bandwidth
	verifyRunning      bool
	verifyWG           sync.WaitGroup
	sourceWG           sync.WaitGroup
	metaWrite          func(string, partMeta) error
	resetPart          func(string) error
	connections        connectionGate
	defaultConnections int
	syncSegments       func(*os.File) error
	verifyFile         func(context.Context, string, string, func(int64, int64)) (VerificationResult, os.FileInfo)
}

type task struct {
	item               Item
	user, pass         string
	headers            http.Header
	customName         bool
	bandwidth          bandwidth
	verifyGeneration   uint64
	verifyCancel       context.CancelFunc
	verifyQueued       bool
	verifyDone         chan struct{}
	sourceGeneration   uint64
	sourceCancel       context.CancelFunc
	sourceCommitting   bool
	sourceAcknowledged uint64
	metaMu             sync.Mutex
	sourceWorking      int
	sourceJobs         sync.WaitGroup

	ctx     context.Context
	cancel  context.CancelFunc
	working bool
	removed bool
	done    chan struct{}

	etag         string
	lastModified string

	lastSample       time.Time
	lastDone         int64
	segmentCommitted int64
	segmentProgress  map[int64]int64
}

// New creates a manager.
func New(opts Options) *Manager {
	if opts.MaxConcurrent < 1 {
		opts.MaxConcurrent = 3
	}
	if opts.MaxConcurrent > 32 {
		opts.MaxConcurrent = 32
	}
	if opts.StallTimeout <= 0 {
		opts.StallTimeout = 90 * time.Second
	}
	if opts.ClientFactory == nil {
		opts.ClientFactory = func() (*http.Client, error) { return &http.Client{}, nil }
	}
	m := &Manager{
		opts:       opts,
		dir:        opts.Dir,
		max:        opts.MaxConcurrent,
		auto:       opts.AutoStart,
		stall:      opts.StallTimeout,
		byID:       map[string]*task{},
		reserved:   map[string]bool{},
		verifyFile: verifyFileSHA256,
		metaWrite:  writePartMeta,
		resetPart:  resetPartFile,
	}
	m.bandwidth.set(opts.RateLimit)
	if opts.MaxConnections < 1 {
		opts.MaxConnections = 16
	}
	m.connections.set(min(opts.MaxConnections, 64))
	m.defaultConnections = max(1, min(opts.DefaultConnections, 8))
	m.syncSegments = func(f *os.File) error { return f.Sync() }
	return m
}

// SetOnChange replaces the change callback, which lets the app attach the
// window after it is created.
func (m *Manager) SetOnChange(fn func()) {
	m.mu.Lock()
	m.opts.OnChange = fn
	m.mu.Unlock()
}

// SetOnComplete replaces the completion callback.
func (m *Manager) SetOnComplete(fn func(Item)) {
	m.mu.Lock()
	m.opts.OnComplete = fn
	m.mu.Unlock()
}

// Add parses the links in raw, one per line or space separated, and queues
// them. It returns how many were added or given new credentials, how many were
// skipped as duplicates of an unfinished download, and invalid tokens.
func (m *Manager) Add(raw string) (added int, skipped int, invalid []string) {
	targets, invalid := parseTargets(raw)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return 0, 0, invalid
	}
	added, skipped = m.addTargetsLocked(targets, m.dir)
	m.mu.Unlock()
	if added > 0 {
		m.kick()
		m.notify()
	}
	return added, skipped, invalid
}

// AddContext adds one imported link through the same admission rules as Add.
// It checks the destination off the manager lock and reports creation failures
// separately from duplicate skips. Call on a worker; an in-flight filesystem
// call finishes before cancellation is observed at the next boundary.
func (m *Manager) AddContext(ctx context.Context, raw string) (added, skipped int, err error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	targets, invalid := parseTargets(raw)
	if len(targets) != 1 || len(invalid) != 0 {
		return 0, 0, fmt.Errorf("请输入一条有效的 HTTP/HTTPS 链接")
	}
	m.mu.Lock()
	dir, closed := m.dir, m.closed
	if !closed && m.unfinishedLocked(targets[0].display) != nil {
		// Refresh/skip existing records independently of the current default
		// directory; their original path is already reserved.
		added, skipped = m.addTargetsLocked(targets, dir)
		m.mu.Unlock()
		if added > 0 {
			m.kick()
			m.notify()
		}
		return added, skipped, nil
	}
	m.mu.Unlock()
	if closed {
		return 0, 0, fmt.Errorf("下载器正在退出")
	}
	dir, err = writableDirectory(dir)
	if err != nil {
		return 0, 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return 0, 0, fmt.Errorf("下载器正在退出")
	}
	added, skipped = m.addTargetsLocked(targets, dir)
	m.mu.Unlock()
	if added > 0 {
		m.kick()
		m.notify()
	}
	return added, skipped, nil
}

func (m *Manager) addTargetsLocked(targets []target, dir string) (added, skipped int) {
	for _, t := range targets {
		if existing := m.unfinishedLocked(t.display); existing != nil {
			// Credentials are intentionally not persisted. Re-adding an
			// authenticated link restores them without losing partial data.
			if !existing.working && !existing.sourceCommitting && t.user != "" && (existing.user != t.user || existing.pass != t.pass) {
				invalidateSourceLocked(existing)
				existing.user, existing.pass = t.user, t.pass
				if m.auto {
					existing.item.State = StateQueued
				}
				added++
			} else {
				skipped++
			}
			continue
		}
		name := "download"
		if u, err := url.Parse(t.display); err == nil {
			name = NameFromURL(u)
		}
		targetPath := uniquePath(dir, name, m.reserved)
		reservePath(m.reserved, targetPath)
		state := StateQueued
		if !m.auto {
			// With auto-start off, a new download waits for the user, as
			// paused, rather than joining the active queue.
			state = StatePaused
		}
		item := Item{
			ConnectionLimit: m.defaultConnections,
			ID:              newID(),
			URL:             t.display,
			DisplayURL:      t.display,
			FileName:        filepath.Base(targetPath),
			Path:            targetPath,
			State:           state,
			Total:           -1,
			CreatedAt:       timeNow(),
		}
		m.byID[item.ID] = &task{item: item, user: t.user, pass: t.pass}
		m.tasks = append(m.tasks, m.byID[item.ID])
		added++
	}
	return added, skipped
}

func (m *Manager) unfinishedLocked(url string) *task {
	for _, t := range m.tasks {
		if t.item.URL == url && !t.item.State.Terminal() {
			return t
		}
	}
	return nil
}

// Pause stops a queued or running download, keeping its partial file.
func (m *Manager) Pause(id string) {
	m.mu.Lock()
	t := m.byID[id]
	if t == nil {
		m.mu.Unlock()
		return
	}
	switch t.item.State {
	case StateQueued, StateConnecting, StateDownloading:
		t.item.State = StatePaused
		t.item.Speed = 0
		t.item.ETA = 0
		if t.cancel != nil {
			t.cancel()
		}
	}
	m.mu.Unlock()
	m.notify()
}

// Resume starts a paused download.
func (m *Manager) Resume(id string) {
	m.request(id)
}

// Retry starts a failed download over, from the bytes already saved when the
// server supports it.
func (m *Manager) Retry(id string) {
	m.request(id)
}

func (m *Manager) request(id string) {
	m.mu.Lock()
	t := m.byID[id]
	if m.closed || t == nil || t.sourceCommitting {
		m.mu.Unlock()
		return
	}
	switch t.item.State {
	case StatePaused, StateFailed:
		invalidateSourceLocked(t)
		t.item.State = StateQueued
		t.item.Error = ""
		t.item.Attempt = 0
		t.item.Missing = false
		t.item.FinishedAt = time.Time{}
	case StateCompleted:
		if !t.item.Missing {
			m.mu.Unlock()
			return
		}
		// The file is gone: download it again.
		resetVerificationLocked(t, t.item.ExpectedSHA256)
		t.item.State = StateQueued
		t.item.Missing = false
		t.item.Error = ""
		t.item.Done = 0
		t.item.Total = -1
		t.item.FinishedAt = time.Time{}
	default:
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.notify()
	m.kick()
}

// Remove forgets a download and deletes its partial file. With deleteFile the
// finished file goes too.
func (m *Manager) Remove(id string, deleteFile bool) {
	m.RemoveMany([]string{id}, deleteFile)
}

// RemoveMany removes several downloads.
func (m *Manager) RemoveMany(ids []string, deleteFiles bool) {
	m.mu.Lock()
	var removed []*task
	for _, id := range ids {
		t := m.byID[id]
		if t == nil {
			continue
		}
		delete(m.byID, id)
		t.removed = true
		invalidateSourceLocked(t)
		cancelVerificationLocked(t)
		if t.cancel != nil {
			t.cancel()
		}
		for i, other := range m.tasks {
			if other == t {
				m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
				break
			}
		}
		removed = append(removed, t)
	}
	m.mu.Unlock()
	for _, t := range removed {
		m.removeFiles(t, deleteFiles)
	}
	m.notify()
}

// removeFiles deletes a removed download's files once its worker has stopped.
func (m *Manager) removeFiles(t *task, deleteFinal bool) {
	m.mu.Lock()
	path := t.item.Path
	working, done := t.working, t.done
	verifyDone := t.verifyDone
	sourceWorking := t.sourceWorking
	m.mu.Unlock()
	if (!working || done == nil) && verifyDone == nil && sourceWorking == 0 {
		deleteFilesFor(path, deleteFinal)
		m.releaseReservation(path)
		return
	}
	go func() {
		if done != nil {
			<-done
		}
		if verifyDone != nil {
			<-verifyDone
		}
		t.sourceJobs.Wait()
		deleteFilesFor(path, deleteFinal)
		m.releaseReservation(path)
	}()
}

func deleteFilesFor(path string, deleteFinal bool) {
	_ = os.Remove(path + ".part.segments")
	_ = os.Remove(path + ".part")
	_ = os.Remove(path + ".part.meta")
	_ = os.Remove(path + ".part.meta.tmp")
	if deleteFinal {
		_ = os.Remove(path)
	}
}

// PauseAll pauses everything that is queued or running.
func (m *Manager) PauseAll() {
	m.mu.Lock()
	for _, t := range m.tasks {
		switch t.item.State {
		case StateQueued, StateConnecting, StateDownloading:
			t.item.State = StatePaused
			t.item.Speed = 0
			t.item.ETA = 0
			if t.cancel != nil {
				t.cancel()
			}
		}
	}
	m.mu.Unlock()
	m.notify()
}

// ResumeAll queues every paused or failed download.
func (m *Manager) ResumeAll() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	for _, t := range m.tasks {
		if t.sourceCommitting {
			continue
		}
		switch t.item.State {
		case StatePaused, StateFailed:
			invalidateSourceLocked(t)
			t.item.State = StateQueued
			t.item.Error = ""
			t.item.Attempt = 0
		case StateCompleted:
			if t.item.Missing {
				resetVerificationLocked(t, t.item.ExpectedSHA256)
				t.item.State = StateQueued
				t.item.Missing = false
				t.item.Error = ""
				t.item.Done = 0
				t.item.Total = -1
				t.item.FinishedAt = time.Time{}
			}
		}
	}
	m.mu.Unlock()
	m.notify()
	m.kick()
}

// ClearCompleted forgets the finished downloads, keeping their files.
func (m *Manager) ClearCompleted() {
	m.mu.Lock()
	var ids []string
	for _, t := range m.tasks {
		if t.item.State == StateCompleted {
			ids = append(ids, t.item.ID)
		}
	}
	m.mu.Unlock()
	m.RemoveMany(ids, false)
}

// SetDir changes where new downloads are saved.
func (m *Manager) SetDir(dir string) {
	if dir == "" {
		return
	}
	m.mu.Lock()
	m.dir = dir
	m.mu.Unlock()
}

// SetMaxConcurrent changes how many downloads run at once.
func (m *Manager) SetMaxConcurrent(n int) {
	if n < 1 {
		n = 1
	}
	if n > 32 {
		n = 32
	}
	m.mu.Lock()
	m.max = n
	m.mu.Unlock()
	m.kick()
}

// SetAutoStart says whether added downloads start at once.
func (m *Manager) SetAutoStart(on bool) {
	m.mu.Lock()
	m.auto = on
	m.mu.Unlock()
}

// RefreshFiles checks completed files. Call it from a background worker: file
// systems such as network shares can block, and Snapshot must stay cheap for UI
// frames. Publish only results that still belong to the same completed task.
func (m *Manager) RefreshFiles() {
	m.mu.Lock()
	var completed []Item
	for _, t := range m.tasks {
		if t.item.State == StateCompleted {
			completed = append(completed, t.item)
		}
	}
	m.mu.Unlock()

	changed := false
	for _, item := range completed {
		info, err := os.Stat(item.Path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
		missing := err != nil || info.IsDir()
		m.mu.Lock()
		t := m.byID[item.ID]
		if t != nil && t.item.State == StateCompleted && t.item.Path == item.Path && t.item.FinishedAt.Equal(item.FinishedAt) {
			if t.item.Missing != missing {
				t.item.Missing = missing
				changed = true
			}
			state := t.item.VerificationState
			verified := state == VerificationMatched || state == VerificationMismatched
			modified := !missing && verified && (info.Size() != t.item.LastVerification.Size || !info.ModTime().Equal(t.item.LastVerification.ModifiedAt))
			if modified || (missing && (verified || state == VerificationChecking || state == VerificationPending)) {
				cancelVerificationLocked(t)
				t.item.VerificationState = VerificationFailed
				t.item.VerificationError = "文件已缺失或发生变化，请重新校验"
				changed = true
			}
		}
		m.mu.Unlock()
	}
	if changed {
		m.notify()
	}
}

// Snapshot returns the downloads and their counts for the UI.
func (m *Manager) Snapshot() ([]Item, Summary) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Item, 0, len(m.tasks))
	var sum Summary
	for _, t := range m.tasks {
		item := t.item
		item.VerificationQueued = t.verifyQueued
		item.SourceUpdating = t.sourceCommitting
		items = append(items, item)
		sum.Total++
		switch item.State {
		case StateQueued:
			sum.Queued++
			sum.Active++
		case StateConnecting, StateDownloading:
			sum.Active++
		case StatePaused:
			sum.Paused++
		case StateCompleted:
			sum.Completed++
		case StateFailed:
			sum.Failed++
		}
		if item.State == StateCompleted {
			sum.DoneBytes += item.Done
		} else {
			sum.DoneBytes += item.Done
			if item.Total > 0 {
				sum.TotalBytes += item.Total
			}
		}
		if item.State == StateDownloading {
			sum.Speed += item.Speed
		}
	}
	return items, sum
}

// Records returns what to save for the next run. Working downloads are saved
// as paused.
func (m *Manager) Records() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.tasks))
	for _, t := range m.tasks {
		state := t.item.State
		if state.Active() {
			state = StatePaused
		}
		out = append(out, Record{
			ConnectionLimit: t.item.ConnectionLimit, TransferMode: t.item.TransferMode,
			ID:               t.item.ID,
			URL:              t.item.URL,
			FileName:         t.item.FileName,
			Path:             t.item.Path,
			State:            state,
			Total:            t.item.Total,
			Done:             t.item.Done,
			TaskRateLimit:    t.item.TaskRateLimit,
			ExpectedSHA256:   t.item.ExpectedSHA256,
			LastVerification: t.item.LastVerification,
			SourceRevision:   t.item.SourceRevision, SourceReset: t.item.SourceReset,
			ETag:            t.etag,
			LastModified:    t.lastModified,
			Resumable:       t.item.Resumable,
			ResumableKnown:  t.item.ResumableKnown,
			CustomName:      t.customName,
			HeadersRequired: len(t.headers) > 0 || t.item.NeedsHeaders,
			RetryCount:      t.item.RetryCount,
			CreatedAt:       t.item.CreatedAt,
			FinishedAt:      t.item.FinishedAt,
		})
		if t.item.TransferMode == "segments" && t.item.State != StateCompleted {
			out[len(out)-1].Done = t.segmentCommitted
		}
	}
	return out
}

// Restore loads saved downloads. Anything unfinished waits, paused, for the
// user to continue it.
func (m *Manager) Restore(records []Record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range records {
		if r.ID == "" || r.URL == "" || r.Path == "" {
			continue
		}
		if _, ok := m.byID[r.ID]; ok {
			continue
		}
		pathConflict := false
		for _, path := range taskPaths(r.Path) {
			if m.reserved[pathKey(path)] {
				pathConflict = true
				break
			}
		}
		if pathConflict {
			// Earlier records own the entire payload/sidecar set. Never reuse or
			// remove a conflicting record's bytes as this task's partial file.
			r.Path = uniquePath(filepath.Dir(r.Path), filepath.Base(r.Path), m.reserved)
			r.State = StatePaused
			r.Done = 0
			r.SourceRevision = 0
			r.SourceReset = false
			r.CustomName = true
			r.FinishedAt = time.Time{}
		}
		name := filepath.Base(r.Path)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = r.FileName
		}
		item := Item{
			ConnectionLimit: max(1, min(r.ConnectionLimit, 8)), TransferMode: r.TransferMode,
			ID:             r.ID,
			URL:            r.URL,
			DisplayURL:     r.URL,
			FileName:       name,
			Path:           r.Path,
			State:          StatePaused,
			Total:          r.Total,
			Done:           r.Done,
			TaskRateLimit:  max(0, min(r.TaskRateLimit, MaxTaskRateLimit)),
			SourceRevision: r.SourceRevision, SourceReset: r.SourceReset,
			CreatedAt:      r.CreatedAt,
			FinishedAt:     r.FinishedAt,
			Resumable:      r.Resumable,
			ResumableKnown: r.ResumableKnown || r.Resumable,
			NeedsHeaders:   r.HeadersRequired,
			RetryCount:     r.RetryCount,
			Missing:        false,
		}
		t := &task{item: item, etag: r.ETag, lastModified: r.LastModified, customName: r.CustomName}
		t.sourceGeneration = r.SourceRevision
		if !r.SourceReset {
			t.sourceAcknowledged = r.SourceRevision
		}
		expected, _ := NormalizeSHA256(r.ExpectedSHA256)
		item.ExpectedSHA256 = expected
		item.LastVerification = r.LastVerification
		if expected != "" {
			item.VerificationState = VerificationPending
		}
		t.bandwidth.set(item.TaskRateLimit)
		meta := readPartMeta(r.Path + ".part.meta")
		if source := meta.Source; validSourceJournal(source, r.ID) && source.Revision >= r.SourceRevision {
			item.URL = source.URL
			item.DisplayURL = source.URL
			item.SourceRevision = source.Revision
			item.SourceReset = source.Reset
			item.NeedsHeaders = source.HeadersRequired
			item.FinishedAt = time.Time{}
			item.Total = meta.Total
			t.sourceGeneration = max(t.sourceGeneration, source.Revision)
			t.customName = true
			t.etag = meta.ETag
			t.lastModified = meta.LastModified
			r.State = StatePaused
			if source.Completed && !source.Reset && fileExists(r.Path) && fileSize(r.Path) == meta.Total {
				r.State = StateCompleted
				item.Done = meta.Total
				item.FinishedAt = source.FinishedAt
			}
		}
		if t.etag == "" {
			t.etag = meta.ETag
		}
		if t.lastModified == "" {
			t.lastModified = meta.LastModified
		}
		if r.State == StateCompleted {
			if fileExists(r.Path) {
				item.State = StateCompleted
			} else {
				item.State = StateCompleted
				item.Missing = true
			}
		} else {
			part := fileSize(r.Path + ".part")
			if item.SourceReset {
				part = 0
			}
			item.Done = part
			if !item.SourceReset && (meta.Mode != "" || meta.SchemaVersion != 0 || fileExists(r.Path+".part.segments")) {
				item.Done = 0
				if validSegments(meta, item.URL) && segmentFileValid(r.Path+".part.segments", meta) {
					item.Done = completedBytes(meta)
					t.segmentCommitted = item.Done
					item.Total = meta.Total
					item.TransferMode = "segments"
				} else {
					item.FallbackReason = "分片元信息缺失、损坏或版本未知，继续时从零恢复"
				}
			}
			if item.Total <= 0 {
				item.Total = meta.Total
			}
			if item.Total <= 0 {
				item.Total = -1
			}
			item.Resumable = item.Resumable || meta.Resumable
		}
		t.item = item
		if pathConflict {
			t.item.FallbackReason = "保存路径与其他任务冲突，已分配新路径"
		}
		reservePath(m.reserved, item.Path)
		m.tasks = append(m.tasks, t)
		m.byID[item.ID] = t
	}
}

// Shutdown stops every worker and leaves the downloads paused. It waits for
// them to release their files.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closed = true
	for _, t := range m.tasks {
		cancelVerificationLocked(t)
		invalidateSourceLocked(t)
		if t.item.State.Active() {
			t.item.State = StatePaused
			t.item.Speed = 0
			t.item.ETA = 0
			if t.cancel != nil {
				t.cancel()
			}
		}
	}
	m.mu.Unlock()
	m.wg.Wait()
	m.verifyWG.Wait()
	m.sourceWG.Wait()
	m.notify()
}

// kick starts queued downloads while there is room.
func (m *Manager) kick() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	started := false
	for m.active < m.max {
		var next *task
		for _, t := range m.tasks {
			if !t.working && t.item.State == StateQueued {
				next = t
				break
			}
		}
		if next == nil {
			break
		}
		next.working = true
		next.item.State = StateConnecting
		next.item.Error = ""
		next.item.FinishedAt = time.Time{}
		next.item.Attempt = 0
		ctx, cancel := context.WithCancel(context.Background())
		next.ctx, next.cancel = ctx, cancel
		next.done = make(chan struct{})
		m.active++
		m.wg.Add(1)
		started = true
		go m.run(next)
	}
	m.mu.Unlock()
	if started {
		m.notify()
	}
}

func (m *Manager) run(t *task) {
	defer m.finishWorker(t)
	err := m.transfer(t.ctx, t)
	if err == nil || errors.Is(err, errRemoved) {
		return
	}
	if t.ctx.Err() != nil {
		return
	}
	m.fail(t, err)
}

func (m *Manager) finishWorker(t *task) {
	m.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	t.working = false
	if m.active > 0 {
		m.active--
	}
	if t.done != nil {
		close(t.done)
		t.done = nil
	}
	if !t.removed && (t.item.State == StateConnecting || t.item.State == StateDownloading) {
		t.item.State = StatePaused
		t.item.Speed = 0
		t.item.ETA = 0
	}
	if t.done != nil {
		close(t.done)
		t.done = nil
	}
	m.mu.Unlock()
	m.wg.Done()
	m.notify()
	m.kick()
	m.kickVerification()
}

func (m *Manager) fail(t *task, err error) {
	m.mu.Lock()
	if t.removed {
		m.mu.Unlock()
		return
	}
	t.item.State = StateFailed
	t.item.Error = taskError(t, err)
	t.item.Speed = 0
	t.item.ETA = 0
	t.item.FinishedAt = timeNow()
	m.mu.Unlock()
	m.notify()
}

// download runs an attempt and its retries.
func (m *Manager) downloadSingle(ctx context.Context, t *task) error {
	const attempts = 3
	restarts := 0
	for attempt := 1; attempt <= attempts; {
		err := m.fetch(ctx, t)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errRemoved) {
			return err
		}
		if errors.Is(err, errRestart) {
			restarts++
			if restarts > 2 {
				return fmt.Errorf("服务器拒绝了续传请求")
			}
			continue
		}
		if !retryable(err) || attempt == attempts {
			return err
		}
		m.setRetrying(t, attempt, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 1500 * time.Millisecond):
		}
		attempt++
	}
	return nil
}

func (m *Manager) setRetrying(t *task, attempt int, err error) {
	m.mu.Lock()
	t.item.State = StateConnecting
	t.item.Attempt = attempt
	t.item.RetryCount++
	t.item.Error = fmt.Sprintf("正在重试（%d/%d）：%s", attempt, 2, taskError(t, err))
	t.item.Speed = 0
	t.item.ETA = 0
	m.mu.Unlock()
	m.notify()
}

func (m *Manager) notify() {
	m.mu.Lock()
	fn := m.opts.OnChange
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (m *Manager) notifyThrottled() {
	now := time.Now().UnixNano()
	last := m.lastNotify.Load()
	if now-last < int64(200*time.Millisecond) {
		return
	}
	if !m.lastNotify.CompareAndSwap(last, now) {
		return
	}
	m.notify()
}

func (m *Manager) current(t *task) Item {
	m.mu.Lock()
	defer m.mu.Unlock()
	return t.item
}

func (m *Manager) releaseReservation(path string) {
	if path == "" {
		return
	}
	m.mu.Lock()
	unreservePath(m.reserved, path)
	m.mu.Unlock()
}
