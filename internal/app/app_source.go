package app

import (
	"context"
	"go-downloader/internal/download"
)

type sourceState struct {
	open, wasOpen, busy, saved bool
	generation                 uint64
	id, url, headers, err      string
	draft                      *download.SourceDraft
	preview                    download.SourcePreview
	ctx                        context.Context
	cancel                     context.CancelFunc
}

func sourceEditable(it download.Item) bool {
	return !it.SourceUpdating && (it.State == download.StatePaused || it.State == download.StateFailed)
}

func (a *app) openSource(id string) {
	if a.stopping {
		return
	}
	a.closeSource()
	a.detailsOpen = false
	ctx, cancel := context.WithCancel(a.lifetime)
	a.source = sourceState{open: true, id: id, generation: a.source.generation, ctx: ctx, cancel: cancel}
}

func (a *app) invalidateSourcePreview() {
	if a.source.draft != nil {
		a.source.draft.Cancel()
	}
	a.source.draft = nil
	a.source.preview = download.SourcePreview{}
}

func (a *app) closeSource() {
	a.invalidateSourcePreview()
	if a.source.cancel != nil {
		a.source.cancel()
	}
	a.source = sourceState{generation: a.source.generation + 1}
}

func (a *app) sourceCurrent(generation uint64) bool {
	return !a.stopping && a.source.open && a.source.generation == generation
}

func (a *app) probeSource() {
	if !a.source.open || a.source.busy || a.source.saved {
		return
	}
	a.invalidateSourcePreview()
	headers, err := download.ParseHeaders(a.source.headers)
	if err != nil {
		a.source.err = err.Error()
		return
	}
	draft, err := a.engine.BeginSourceUpdate(a.source.id, download.SourceOptions{URL: a.source.url, Headers: headers})
	if err != nil {
		a.source.err = err.Error()
		return
	}
	a.source.draft = draft
	a.source.busy, a.source.err = true, ""
	ctx, generation, engine := a.source.ctx, a.source.generation, a.engine
	a.backgroundJobs.Add(1)
	a.deps.async(func() {
		defer a.backgroundJobs.Done()
		preview, err := engine.ProbeSource(ctx, draft)
		a.dispatch(func() {
			if !a.sourceCurrent(generation) {
				return
			}
			a.source.busy = false
			if err != nil {
				a.source.err = err.Error()
				a.invalidateSourcePreview()
				return
			}
			a.source.preview = preview
		})
	})
}

func (a *app) commitSource() {
	if !a.source.open || a.source.busy || a.source.saved || a.source.draft == nil || a.source.preview.URL == "" {
		return
	}
	draft, generation, engine := a.source.draft, a.source.generation, a.engine
	a.source.busy, a.source.err = true, ""
	a.backgroundJobs.Add(1)
	a.deps.async(func() {
		defer a.backgroundJobs.Done()
		err := engine.CommitSource(draft)
		var saveErr error
		if err == nil {
			a.dirty.Store(true)
			saveErr = a.persistTasks()
		}
		a.dispatch(func() {
			if !a.sourceCurrent(generation) {
				return
			}
			a.source.busy = false
			if err != nil {
				a.source.err = err.Error()
				a.invalidateSourcePreview()
				return
			}
			a.source.saved = true
			a.source.url, a.source.headers, a.source.draft = "", "", nil
			if saveErr != nil {
				a.source.err = "链接已更新；任务清单保存失败，可恢复元信息已保留。"
			}
		})
	})
}

func (a *app) continueSource() {
	if !a.source.saved || a.source.busy {
		return
	}
	id := a.source.id
	a.closeSource()
	a.engine.Resume(id)
}
