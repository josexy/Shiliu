package app

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
	"testing"
	"time"
)

func (f *fakeEngine) SetSchedule(id string, s download.Schedule) error {
	if err := s.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.items {
		if f.items[i].ID == id {
			f.items[i].Schedule = s
			f.items[i].ManualPaused = !s.Enabled
			return nil
		}
	}
	return fmt.Errorf("任务已移除")
}

func (f *fakeEngine) StartScheduling() {}

func TestScheduleEditorValidationSaveAndReopen(t *testing.T) {
	it := seedItem("scheduled", "planned.bin", download.StatePaused)
	f := &fakeEngine{items: []download.Item{it}}
	a := newTestApp(t, f)
	a.openDetails(it.ID)
	tt := testerFor(a)
	tt.SetSize(960, 640)
	tt.SetScale(1.25)
	if err := tt.Click("下载计划…"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"启用下载计划", "计划开始时间"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	tt.Type("bad time")
	if err := tt.Click("保存计划"); err != nil {
		t.Fatal(err)
	}
	if a.scheduleEditor.err == "" || f.items[0].Schedule.Enabled {
		t.Fatal("invalid plan applied")
	}
	if err := tt.Click("计划开始时间"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("2027-01-02 22:00")
	if err := tt.Click("限定每日下载时段"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("每日结束时间"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("22:00")
	if err := tt.Click("保存计划"); err != nil {
		t.Fatal(err)
	}
	if a.scheduleEditor.err == "" {
		t.Fatal("equal daily boundaries accepted")
	}
	if err := tt.Click("每日结束时间"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("06:00")
	if err := tt.Click("保存计划"); err != nil {
		t.Fatal(err)
	}
	want, _ := time.ParseInLocation("2006-01-02 15:04", "2027-01-02 22:00", time.Local)
	s := f.items[0].Schedule
	if !s.Enabled || !s.StartAt.Equal(want) || s.DailyStartMinute != 1320 || s.DailyEndMinute != 360 || a.scheduleEditor.open {
		t.Fatal(s)
	}
	if err := tt.Click("下载计划…"); err != nil {
		t.Fatal(err)
	}
	if a.scheduleEditor.draft.start != "2027-01-02 22:00" {
		t.Fatal("reopened draft lost time")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.scheduleEditor.open {
		t.Fatal("Escape failed")
	}
}

func TestAdvancedScheduleLateResultDoesNotOverwriteNewDraft(t *testing.T) {
	f := &fakeEngine{}
	a := newTestApp(t, f)
	var work func()
	a.deps.async = func(fn func()) { work = fn }
	a.openAdvanced()
	a.advanced.url = "https://example.com/old"
	a.advanced.schedule.enabled = true
	a.advanced.schedule.start = "2027-01-02 22:00"
	a.submitAdvanced()
	if work == nil {
		t.Fatal(a.advanced.err)
	}
	a.clearAdvanced()
	a.openAdvanced()
	a.advanced.url = "https://example.com/new"
	a.advanced.schedule.start = "2028-01-01 08:00"
	work()
	if !a.advanced.open || a.advanced.url != "https://example.com/new" || a.advanced.schedule.start != "2028-01-01 08:00" {
		t.Fatal("late result overwrote new editor")
	}
	if len(f.advancedOptions) != 1 || f.advancedOptions[0].Schedule == nil || !f.advancedOptions[0].Schedule.Enabled {
		t.Fatal("plan not captured with submission")
	}
}
