package download

import (
	"fmt"
	"time"
)

// Schedule stores the one-time threshold as an absolute instant. Daily minute
// boundaries are interpreted in the clock's current local zone, including DST.
type Schedule struct {
	Enabled          bool      `json:"enabled,omitempty"`
	StartAt          time.Time `json:"startAt,omitzero"`
	StartReached     bool      `json:"startReached,omitempty"`
	DailyEnabled     bool      `json:"dailyEnabled,omitempty"`
	DailyStartMinute int       `json:"dailyStartMinute,omitempty"`
	DailyEndMinute   int       `json:"dailyEndMinute,omitempty"`
}

func (s Schedule) Validate() error {
	if s.DailyEnabled && (s.DailyStartMinute < 0 || s.DailyStartMinute >= 1440 || s.DailyEndMinute < 0 || s.DailyEndMinute >= 1440 || s.DailyStartMinute == s.DailyEndMinute) {
		return fmt.Errorf("每日时段须为有效的 HH:mm，起止不能相同")
	}
	return nil
}

// ScheduleEligibility is pure. A reached start condition remains reached even
// after a clock rollback; missed daily windows are never replayed.
func ScheduleEligibility(s Schedule, now time.Time) (reached bool, waiting string) {
	reached = s.StartReached || s.StartAt.IsZero() || !now.Before(s.StartAt)
	if !reached {
		return false, "等待开始时间"
	}
	if s.DailyEnabled {
		minute := now.Hour()*60 + now.Minute()
		allowed := minute >= s.DailyStartMinute && minute < s.DailyEndMinute
		if s.DailyStartMinute > s.DailyEndMinute {
			allowed = minute >= s.DailyStartMinute || minute < s.DailyEndMinute
		}
		if !allowed {
			return true, "等待每日允许时段"
		}
	}
	return true, ""
}

// Clock and Timer make time changes and cancellation deterministic in tests.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Duration) bool
}
type realClock struct{}

func (realClock) Now() time.Time { return scheduleNow() }
func (realClock) NewTimer(d time.Duration) Timer {
	refreshScheduleZone()
	return realTimer{time.NewTimer(d)}
}

type realTimer struct{ *time.Timer }

func (t realTimer) C() <-chan time.Time        { return t.Timer.C }
func (t realTimer) Reset(d time.Duration) bool { refreshScheduleZone(); return t.Timer.Reset(d) }

func (m *Manager) SetSchedule(id string, schedule Schedule) error {
	if err := schedule.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	t := m.byID[id]
	if m.closed || t == nil {
		m.mu.Unlock()
		return fmt.Errorf("任务已移除或下载器正在退出")
	}
	if t.sourceCommitting {
		m.mu.Unlock()
		return fmt.Errorf("正在更新链接，请稍后重试")
	}
	m.setScheduleLocked(t, schedule)
	m.mu.Unlock()
	m.kick()
	m.notify()
	return nil
}

func (m *Manager) setScheduleLocked(t *task, s Schedule) {
	s.StartReached = t.item.Schedule.StartReached && s.StartAt.Equal(t.item.Schedule.StartAt)
	if !s.StartAt.IsZero() {
		s.StartAt = s.StartAt.UTC()
	}
	t.item.Schedule = s
	t.item.ManualPaused = !s.Enabled
	t.item.ScheduleWaiting = ""
	if !s.Enabled && !t.item.State.Terminal() {
		m.pauseForScheduleLocked(t)
	}
}

func (m *Manager) pauseForScheduleLocked(t *task) {
	m.setStateLocked(t, StatePaused)
	t.item.Speed, t.item.ETA = 0, 0
	if t.cancel != nil {
		t.cancel()
	}
}

func (m *Manager) evaluateSchedulesLocked(now time.Time) bool {
	if m.opts.DeferScheduling {
		return false
	}
	changed := false
	for _, t := range m.tasks {
		if !t.item.Schedule.Enabled {
			continue
		}
		m.ensureSchedulerLocked()
		if t.item.State.Terminal() || t.sourceCommitting {
			continue
		}
		reached, waiting := ScheduleEligibility(t.item.Schedule, now)
		if reached != t.item.Schedule.StartReached {
			t.item.Schedule.StartReached = reached
			changed = true
		}
		if t.item.ManualPaused {
			waiting = "已手动暂停"
		}
		if waiting != t.item.ScheduleWaiting {
			t.item.ScheduleWaiting = waiting
			changed = true
		}
		if waiting != "" && t.item.State.Active() {
			m.pauseForScheduleLocked(t)
			changed = true
		}
		if waiting == "" && t.item.State == StatePaused {
			m.setStateLocked(t, StateQueued)
			changed = true
		}
	}
	return changed
}

// StartScheduling releases the startup barrier once proxy credentials, system
// settings, and UI dispatch are ready. Ordinary managers have no barrier.
func (m *Manager) StartScheduling() {
	m.mu.Lock()
	m.opts.DeferScheduling = false
	m.mu.Unlock()
	m.kick()
}

func (m *Manager) ensureSchedulerLocked() {
	if m.scheduleStop != nil {
		return
	}
	m.scheduleStop = make(chan struct{})
	m.scheduleWG.Add(1)
	go func() {
		defer m.scheduleWG.Done()
		timer := m.clock.NewTimer(m.scheduleDelay())
		defer timer.Stop()
		for {
			select {
			case <-m.scheduleStop:
				return
			case <-timer.C():
				m.sampleHistories()
				m.kick()
				timer.Reset(m.scheduleDelay())
			}
		}
	}()
}

// Re-evaluate at the nearest sub-second threshold/minute boundary, and at
// least once a second to notice system clock, local zone, or sleep changes.
func (m *Manager) scheduleDelay() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock.Now()
	delay := min(time.Second, time.Minute-time.Duration(now.Second())*time.Second-time.Duration(now.Nanosecond()))
	for _, t := range m.tasks {
		if s := t.item.Schedule; s.Enabled && !s.StartReached && now.Before(s.StartAt) {
			delay = min(delay, s.StartAt.Sub(now))
		}
	}
	return max(10*time.Millisecond, delay)
}
