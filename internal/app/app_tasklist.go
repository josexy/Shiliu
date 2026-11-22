package app

import (
	"context"
	"fmt"
	"go-downloader/internal/download"

	"github.com/egoist/mygo/ui"

	"go-downloader/internal/tasklist"
)

type taskListState struct {
	drop                                  bool
	batchID                               uint64
	open, wasOpen, export, busy, finished bool
	generation                            uint64
	ctx                                   context.Context
	cancel                                context.CancelFunc
	preview                               tasklist.Preview
	list                                  ui.ListState
	message, err                          string
}

func (a *app) openTaskList(export bool) {
	a.closeTaskList()
	ctx, cancel := context.WithCancel(a.lifetime)
	a.taskList = taskListState{open: true, export: export, generation: a.taskList.generation, ctx: ctx, cancel: cancel}
}

func (a *app) openImport() {
	if !a.stopping {
		a.openTaskList(false)
	}
}
func (a *app) openExport() {
	if !a.stopping {
		a.openTaskList(true)
	}
}

func (a *app) closeTaskList() {
	if a.taskList.drop && a.restoreQuickInput {
		a.focusQuickInput = true
	}
	if a.taskList.cancel != nil {
		a.taskList.cancel()
	}
	a.taskList = taskListState{generation: a.taskList.generation + 1}
}

func (a *app) taskListCurrent(generation uint64) bool {
	return !a.stopping && a.taskList.open && a.taskList.generation == generation
}

func (a *app) chooseTaskList() {
	if a.taskList.busy || a.deps.pickTaskList == nil {
		return
	}
	a.taskList.busy, a.taskList.err = true, ""
	generation, win, pick := a.taskList.generation, a.win, a.deps.pickTaskList
	// Native dialogs may remain open until the OS dismisses them. Do not wait
	// for one during shutdown; its late result is rejected on the UI thread.
	a.deps.async(func() {
		path, err := pick(win)
		a.dispatch(func() {
			if !a.taskListCurrent(generation) {
				return
			}
			a.taskList.busy = false
			if err != nil {
				a.taskList.err = "无法打开文件选择器"
				return
			}
			if path == "" {
				return
			}
			a.readTaskList(path)
		})
	})
}

func (a *app) readTaskList(path string) {
	ctx, generation := a.taskList.ctx, a.taskList.generation
	items, _ := a.engine.Snapshot()
	a.taskList.busy = true
	a.taskListJobs.Add(1)
	a.deps.async(func() {
		defer a.taskListJobs.Done()
		preview, err := tasklist.Read(ctx, path, items)
		a.dispatch(func() {
			if !a.taskListCurrent(generation) {
				return
			}
			a.taskList.busy = false
			if err != nil {
				a.taskList.err = "无法读取 TXT：请检查文件权限、UTF-8 编码和容量限制"
				return
			}
			a.taskList.preview, a.taskList.err = preview, ""
			a.taskList.list = ui.ListState{}
		})
	})
}

type importResult struct {
	added, skipped, failed, remaining int
	errors                            []string
}

// submitImportCandidates rechecks duplicates through the normal Add path for
// every candidate. Successful tasks survive later errors or cancellation.
func submitImportCandidates(ctx context.Context, engine downloader, candidates []string) importResult {
	result := importResult{}
	for i, raw := range candidates {
		if ctx.Err() != nil {
			result.remaining = len(candidates) - i
			break
		}
		added, skipped, err := engine.AddContext(ctx, raw)
		result.added += added
		result.skipped += skipped
		if err != nil || added+skipped == 0 {
			result.failed++
			if len(result.errors) < 10 {
				message := fmt.Sprintf("第 %d 个有效候选创建失败", i+1)
				if err != nil {
					message += "：" + download.DiagnosticText(err.Error())
				}
				result.errors = append(result.errors, message)
			}
		}
	}
	return result
}

func (a *app) confirmImport() {
	if a.taskList.busy || a.taskList.finished || !a.taskList.open {
		return
	}
	candidates := a.taskList.preview.Candidates()
	if len(candidates) == 0 {
		return
	}
	ctx, generation, engine := a.taskList.ctx, a.taskList.generation, a.engine
	a.taskList.busy = true
	a.taskListJobs.Add(1)
	a.deps.async(func() {
		defer a.taskListJobs.Done()
		result := submitImportCandidates(ctx, engine, candidates)
		a.dispatch(func() {
			if !a.taskListCurrent(generation) {
				return
			}
			a.taskList.busy, a.taskList.finished = false, true
			a.taskList.preview = tasklist.Preview{}
			a.taskList.message = fmt.Sprintf("已添加 %d 项 · 跳过 %d 项 · 失败 %d 项 · 未处理 %d 项", result.added, result.skipped, result.failed, result.remaining)
			for _, message := range result.errors {
				a.taskList.message += "\n" + message
			}
			a.dirty.Store(true)
		})
	})
}

func (a *app) exportTaskList(selectedOnly bool) {
	if a.taskList.busy || a.deps.saveTaskList == nil {
		return
	}
	items, _ := a.engine.Snapshot()
	var selected map[string]bool
	if selectedOnly {
		selected = map[string]bool{}
		for id := range a.selection.All() {
			selected[id] = true
		}
	}
	ctx, generation, win, pick := a.taskList.ctx, a.taskList.generation, a.win, a.deps.saveTaskList
	a.taskList.busy, a.taskList.err = true, ""
	a.deps.async(func() {
		path, err := pick(win)
		a.dispatch(func() {
			if !a.taskListCurrent(generation) {
				return
			}
			a.taskList.busy = false
			if err != nil {
				a.taskList.err = "无法打开保存选择器"
				return
			}
			if path == "" {
				return
			}
			a.taskList.busy = true
			a.taskListJobs.Add(1)
			a.deps.async(func() {
				defer a.taskListJobs.Done()
				data, err := tasklist.Export(items, selected)
				if err == nil {
					err = tasklist.Write(ctx, path, data)
				}
				a.dispatch(func() {
					if !a.taskListCurrent(generation) {
						return
					}
					a.taskList.busy = false
					if err != nil {
						a.taskList.err = "导出失败，请检查目标目录和文件权限；原文件未被替换"
						return
					}
					a.taskList.finished = true
					a.taskList.message = "脱敏任务清单已保存"
				})
			})
		})
	})
}
