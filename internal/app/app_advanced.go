package app

import (
	"strings"

	"go-downloader/internal/download"
)

type advancedState struct {
	schedule    scheduleDraft
	open        bool
	busy        bool
	generation  uint64
	url         string
	dir         string
	dirOverride bool
	fileName    string
	referer     string
	cookie      string
	headers     string
	rateKiB     string
	connections string
	expectedSHA string
	err         string
}

func (a *app) openAdvanced() {
	generation := a.advanced.generation + 1
	a.advanced = advancedState{open: true, generation: generation, dir: a.s.DownloadDir, rateKiB: "0"}
	a.advanced.connections = numberLabel(a.s.TaskConnections)
	a.advanced.schedule = newScheduleDraft(download.Schedule{})
	if valid, invalid := download.NormalizeURLs(a.input); len(valid) == 1 && len(invalid) == 0 {
		// Keep in-memory URL credentials when pre-filling the advanced form.
		a.advanced.url = strings.TrimSpace(a.input)
	}
}

func (a *app) clearAdvanced() {
	a.advanced = advancedState{generation: a.advanced.generation + 1}
	a.advancedWasOpen = false
}

func (a *app) chooseAdvancedFolder() {
	if a.deps.pickFolder == nil {
		return
	}
	generation, win := a.advanced.generation, a.win
	a.deps.async(func() {
		dir, err := a.deps.pickFolder(win, "选择此任务的保存文件夹")
		a.dispatch(func() {
			if !a.advanced.open || a.advanced.generation != generation {
				return
			}
			if err != nil {
				a.advanced.err = "无法选择文件夹：" + err.Error()
			} else if dir != "" {
				a.advanced.dir = dir
				a.advanced.dirOverride = true
			}
		})
	})
}

func (a *app) submitAdvanced() {
	if a.advanced.busy {
		return
	}
	schedule, err := a.advanced.schedule.parse()
	if err != nil {
		a.advanced.err = err.Error()
		return
	}
	connections, err := download.ParseConnectionCount(a.advanced.connections, 8)
	if err != nil {
		a.advanced.err = err.Error()
		return
	}
	rate, err := download.ParseTaskRateKiB(a.advanced.rateKiB)
	if err != nil {
		a.advanced.err = err.Error()
		return
	}
	expected, err := download.NormalizeSHA256(a.advanced.expectedSHA)
	if err != nil {
		a.advanced.err = err.Error()
		return
	}
	headers, err := download.ParseHeaders(a.advanced.headers)
	if err != nil {
		a.advanced.err = err.Error()
		return
	}
	if a.advanced.referer != "" {
		headers.Set("Referer", a.advanced.referer)
	}
	if a.advanced.cookie != "" {
		headers.Set("Cookie", a.advanced.cookie)
	}
	opts := download.AddOptions{URL: a.advanced.url, Dir: a.advanced.dir,
		DirOverride: a.advanced.dirOverride,
		FileName:    a.advanced.fileName, Headers: headers, TaskRateLimit: rate, ExpectedSHA256: expected, ConnectionLimit: connections}
	if schedule.Enabled || !schedule.StartAt.IsZero() || schedule.DailyEnabled {
		opts.Schedule = &schedule
	}
	a.advanced.busy, a.advanced.err = true, ""
	generation, engine := a.advanced.generation, a.engine
	a.deps.async(func() {
		_, err := engine.AddTask(opts)
		a.dispatch(func() {
			if a.advanced.generation != generation || !a.advanced.open {
				if err == nil {
					a.toast("已添加或更新下载任务")
				}
				return
			}
			a.advanced.busy = false
			if err != nil {
				a.advanced.err = err.Error()
				return
			}
			a.clearAdvanced()
			a.dirty.Store(true)
			a.toast("已添加或更新下载任务")
		})
	})
}
