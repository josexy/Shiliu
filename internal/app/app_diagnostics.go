package app

import (
	"context"
	"go-downloader/internal/diagnostics"
)

type diagnosticExportState struct {
	busy       bool
	generation uint64
	cancel     context.CancelFunc
	message    string
}

func (a *app) cancelDiagnosticExport() {
	a.diagnosticExport.generation++
	if a.diagnosticExport.cancel != nil {
		a.diagnosticExport.cancel()
		a.diagnosticExport.cancel = nil
	}
	a.diagnosticExport.busy = false
	a.diagnosticExport.message = "已取消导出"
}
func (a *app) exportDiagnostics() {
	if a.stopping || a.diagnosticExport.busy || a.deps.saveDiagnostics == nil {
		return
	}
	a.diagnosticExport.generation++
	generation := a.diagnosticExport.generation
	ctx, cancel := context.WithCancel(a.lifetime)
	a.diagnosticExport.cancel = cancel
	a.diagnosticExport.busy = true
	a.diagnosticExport.message = "正在选择保存位置…"
	win, pick := a.win, a.deps.saveDiagnostics
	// A native save picker is not awaited at shutdown; its late result is
	// rejected before starting any tracked file work.
	a.deps.async(func() {
		path, err := pick(win)
		a.dispatch(func() {
			if a.stopping || generation != a.diagnosticExport.generation || ctx.Err() != nil {
				return
			}
			if err != nil || path == "" {
				a.cancelDiagnosticExport()
				if err != nil {
					a.diagnosticExport.message = "无法打开保存选择器"
				}
				return
			}
			snapshot := a.diagnostics.Snapshot()
			version := a.deps.version
			a.diagnosticExport.message = "正在导出…"
			a.backgroundJobs.Add(1)
			a.deps.async(func() {
				defer a.backgroundJobs.Done()
				defer cancel()
				err := diagnostics.Export(ctx, path, version, snapshot)
				a.dispatch(func() {
					if a.stopping || generation != a.diagnosticExport.generation {
						return
					}
					a.diagnosticExport.busy = false
					a.diagnosticExport.cancel = nil
					if err != nil {
						a.diagnosticExport.message = "导出失败，请检查目标目录和文件权限"
					} else {
						a.diagnosticExport.message = "诊断日志已导出"
					}
				})
			})
		})
	})
}
