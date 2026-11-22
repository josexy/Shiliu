package download

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type SourceOptions struct {
	URL     string
	Headers http.Header
}

// SourcePreview contains display-safe information only. Credentials remain in
// the private, cancelable draft until commit and are never persisted with it.
type SourcePreview struct {
	URL          string
	Reuse        bool
	Bytes, Total int64
	Reason       string
}

type SourceDraft struct {
	mu                 sync.Mutex
	manager            *Manager
	id                 string
	generation         uint64
	old                Item
	target             target
	headers            http.Header
	ctx                context.Context
	cancel             context.CancelFunc
	probing, validated bool
	preview            SourcePreview
	partInfo           os.FileInfo
	partPath           string
	meta               partMeta
}

func (d *SourceDraft) Cancel() {
	if d != nil && d.cancel != nil {
		d.cancel()
	}
}

func invalidateSourceLocked(t *task) {
	t.sourceGeneration++
	if t.sourceCancel != nil {
		t.sourceCancel()
		t.sourceCancel = nil
	}
}

func (m *Manager) sourceTaskLocked(id string) (*task, error) {
	t := m.byID[id]
	if m.closed || t == nil || t.removed {
		return nil, fmt.Errorf("任务已移除或下载器正在退出")
	}
	if t.working || t.sourceCommitting || (t.item.State != StatePaused && t.item.State != StateFailed) {
		return nil, fmt.Errorf("只能更新已停止的暂停或失败任务，请先等待下载停止")
	}
	return t, nil
}

func (m *Manager) sourceDuplicateLocked(id, url string) bool {
	for _, t := range m.tasks {
		if t.item.ID != id && !t.removed && t.item.URL == url && (t.item.State != StateCompleted || t.item.Missing) {
			return true
		}
	}
	return false
}

func (m *Manager) BeginSourceUpdate(id string, opts SourceOptions) (*SourceDraft, error) {
	target, err := parseTarget(strings.TrimSpace(opts.URL))
	if err != nil {
		return nil, fmt.Errorf("请输入一条有效的 HTTP/HTTPS 链接")
	}
	headers, err := validateHeaders(opts.Headers)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.sourceTaskLocked(id)
	if err != nil {
		return nil, err
	}
	if m.sourceDuplicateLocked(id, target.display) {
		return nil, fmt.Errorf("新链接与另一个未完成任务重复")
	}
	invalidateSourceLocked(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.sourceCancel = cancel
	return &SourceDraft{manager: m, id: id, generation: t.sourceGeneration, old: t.item, target: target, headers: headers, ctx: ctx, cancel: cancel}, nil
}

// CommitSource only accepts a successfully probed draft whose task is still
// stopped. It preserves task identity, destination and unrelated settings.
// Updating does not start the task: resuming is a separate explicit operation.
func (m *Manager) CommitSource(d *SourceDraft) error {
	if d == nil || d.manager != m {
		return fmt.Errorf("无效的链接更新草稿")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.probing || !d.validated || d.ctx.Err() != nil {
		return fmt.Errorf("请先重新验证新链接")
	}
	m.mu.Lock()
	t, err := m.sourceTaskLocked(d.id)
	if err == nil && t.sourceGeneration != d.generation {
		err = fmt.Errorf("任务已发生变化，请重新验证新链接")
	}
	if err == nil && m.sourceDuplicateLocked(d.id, d.target.display) {
		err = fmt.Errorf("新链接与另一个未完成任务重复")
	}
	if err != nil {
		m.mu.Unlock()
		return err
	}
	t.sourceCommitting = true
	t.sourceWorking++
	t.sourceJobs.Add(1)
	m.sourceWG.Add(1)
	m.mu.Unlock()
	defer func() { m.mu.Lock(); t.sourceCommitting = false; m.mu.Unlock(); m.sourceJobDone(t, true); m.notify() }()
	t.metaMu.Lock()
	defer t.metaMu.Unlock()
	if d.ctx.Err() != nil {
		return fmt.Errorf("链接更新已取消")
	}
	if !sourcePartUnchanged(d) {
		return fmt.Errorf("部分文件在验证后发生变化，请重新验证")
	}
	meta := partMeta{URL: d.target.display, Total: d.preview.Total, Resumable: d.preview.Reuse}
	if d.preview.Reuse {
		meta.ETag = d.meta.ETag
		if d.meta.Mode == "segments" {
			meta = d.meta
			meta.URL = d.target.display
		}
	}
	meta.Source = &sourceJournal{TaskID: d.id, Revision: d.generation, URL: d.target.display, Reset: !d.preview.Reuse && d.partInfo != nil && d.partInfo.Size() > 0, HeadersRequired: len(d.headers) > 0}
	if err := m.metaWrite(d.old.Path+".part.meta", meta); err != nil {
		return fmt.Errorf("无法提交新链接元信息：%w", err)
	}
	// The atomic journal replacement is the commit point. Cancellation after
	// it cannot roll back the durable intent; shutdown waits for publication.
	m.mu.Lock()
	if t.removed {
		m.mu.Unlock()
		return errRemoved
	}
	m.commitSourceLocked(t, d)
	m.mu.Unlock()
	return nil
}

func (m *Manager) commitSourceLocked(t *task, d *SourceDraft) {
	t.item.URL, t.item.DisplayURL = d.target.display, d.target.display
	t.user, t.pass = d.target.user, d.target.pass
	t.headers = d.headers.Clone()
	t.customName = true
	t.item.NeedsHeaders = false
	t.item.SourceRevision = d.generation
	t.item.SourceReset = !d.preview.Reuse && d.partInfo != nil && d.partInfo.Size() > 0
	m.setStateLocked(t, StatePaused)
	t.item.Error = ""
	t.item.Speed = 0
	t.item.ETA = 0
	t.item.FinishedAt = time.Time{}
	t.etag, t.lastModified = "", ""
	t.item.Done = 0
	t.segmentCommitted = 0
	t.item.TransferMode = "single"
	t.item.Total = d.preview.Total
	t.item.Resumable = false
	t.item.ResumableKnown = true
	if d.preview.Reuse {
		t.item.Done = d.preview.Bytes
		t.etag = d.meta.ETag
		t.item.Resumable = true
		if d.meta.Mode == "segments" {
			t.item.TransferMode = "segments"
			t.segmentCommitted = d.preview.Bytes
		}
	}
	resetVerificationLocked(t, t.item.ExpectedSHA256)
	invalidateSourceLocked(t)
}
