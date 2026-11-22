package app

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
	"strings"
	"time"
)

type scheduleDraft struct {
	enabled, daily     bool
	start, from, until string
}
type scheduleEditorState struct {
	open  bool
	id    string
	draft scheduleDraft
	err   string
}

func newScheduleDraft(s download.Schedule) scheduleDraft {
	d := scheduleDraft{enabled: s.Enabled, daily: s.DailyEnabled, from: "22:00", until: "06:00"}
	if !s.StartAt.IsZero() {
		d.start = download.FormatScheduleStart(s.StartAt)
	}
	if s.DailyEnabled {
		d.from = fmt.Sprintf("%02d:%02d", s.DailyStartMinute/60, s.DailyStartMinute%60)
		d.until = fmt.Sprintf("%02d:%02d", s.DailyEndMinute/60, s.DailyEndMinute%60)
	}
	return d
}

func (d scheduleDraft) parse() (download.Schedule, error) {
	s := download.Schedule{Enabled: d.enabled, DailyEnabled: d.daily}
	if raw := strings.TrimSpace(d.start); raw != "" {
		parsed, err := download.ParseScheduleStart(raw)
		if err != nil {
			return s, fmt.Errorf("开始时间请填写有效的本地日期时间，例如 2026-12-01 22:00")
		}
		s.StartAt = parsed.UTC()
	}
	if d.daily {
		from, err1 := time.Parse("15:04", strings.TrimSpace(d.from))
		until, err2 := time.Parse("15:04", strings.TrimSpace(d.until))
		if err1 != nil || err2 != nil {
			return s, fmt.Errorf("每日时段请使用 HH:mm，例如 22:00 至 06:00")
		}
		s.DailyStartMinute, s.DailyEndMinute = from.Hour()*60+from.Minute(), until.Hour()*60+until.Minute()
	}
	return s, s.Validate()
}

func scheduleStatus(it download.Item) string {
	if !it.Schedule.Enabled {
		return "未启用"
	}
	if it.ManualPaused {
		return "已手动暂停，继续后按计划运行"
	}
	if it.ScheduleWaiting != "" {
		return it.ScheduleWaiting
	}
	if it.State.Terminal() {
		return "计划已停止（完成或失败不自动重新下载）"
	}
	return "已启用，当前允许下载"
}

func (a *app) openSchedule(it download.Item) {
	a.scheduleEditor = scheduleEditorState{open: true, id: it.ID, draft: newScheduleDraft(it.Schedule)}
}

func (a *app) saveSchedule() {
	s, err := a.scheduleEditor.draft.parse()
	if err == nil {
		err = a.engine.SetSchedule(a.scheduleEditor.id, s)
	}
	if err != nil {
		a.scheduleEditor.err = err.Error()
		return
	}
	a.scheduleEditor = scheduleEditorState{}
	a.dirty.Store(true)
	a.toast("下载计划已保存")
}

func (a *app) scheduleFields(c *ui.Context, key string, d *scheduleDraft) {
	ui.Column(c).Gap(10).Children(func() {
		c.Key(key + "-schedule-enabled")
		ui.Checkbox(c, &d.enabled, "启用下载计划")
		ui.Field(c, "最早开始时间（当前本地时间，可留空）", func() {
			c.Key(key + "-schedule-start")
			ui.TextInput(c, &d.start).Label("计划开始时间").Placeholder("YYYY-MM-DD HH:mm")
		})
		c.Key(key + "-schedule-daily")
		ui.Checkbox(c, &d.daily, "限定每日下载时段")
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			c.Key(key + "-schedule-from")
			ui.TextInput(c, &d.from).Label("每日开始时间").Disabled(!d.daily).Grow(1)
			ui.Text(c, "至")
			c.Key(key + "-schedule-until")
			ui.TextInput(c, &d.until).Label("每日结束时间").Disabled(!d.daily).Grow(1)
		})
		ui.Text(c, "时段包含起点、不包含终点，可跨午夜；仅在应用运行时调度，手动暂停优先。").FontSize(12).TextColor(a.palette.Muted)
	})
}

func (a *app) scheduleModal(c *ui.Context) {
	if !a.scheduleEditor.open {
		return
	}
	p := a.palette
	width, height := c.Size()
	c.Key("schedule-dialog-" + a.scheduleEditor.id)
	ui.Modal(c, &a.scheduleEditor.open, func() {
		ui.Column(c).Width(min(520, width-100)).Gap(14).Children(func() {
			ui.Text(c, "下载计划").FontSize(20).Bold()
			ui.Scroll(c).Height(min(310, height-250)).Children(func() { a.scheduleFields(c, "edit", &a.scheduleEditor.draft) })
			if a.scheduleEditor.err != "" {
				ui.Text(c, a.scheduleEditor.err).TextColor(p.Danger).FontSize(12)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消计划编辑").OnClick(func() { a.scheduleEditor = scheduleEditorState{} })
				ui.PrimaryButton(c, "保存计划").OnClick(a.saveSchedule)
			})
		})
	}).Background(p.Card).Radius(14).Border(1, p.Border)
}
