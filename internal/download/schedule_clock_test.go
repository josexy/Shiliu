package download

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type scheduleClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*scheduleTimer
}
type scheduleTimer struct {
	clock *scheduleClock
	ch    chan time.Time
	armed bool
}

func (c *scheduleClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *scheduleClock) NewTimer(time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &scheduleTimer{c, make(chan time.Time, 1), true}
	c.timers = append(c.timers, timer)
	return timer
}
func (t *scheduleTimer) C() <-chan time.Time { return t.ch }
func (t *scheduleTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	old := t.armed
	t.armed = false
	return old
}
func (t *scheduleTimer) Reset(time.Duration) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	old := t.armed
	t.armed = true
	return old
}

func scheduleEventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("schedule condition timeout")
		}
		time.Sleep(time.Millisecond)
	}
}

// Advance wall time while allowing one timer interval of monotonic time. This
// also models a backward wall-clock change and a timer delivered after sleep.
func (c *scheduleClock) advance(t *testing.T, now time.Time) {
	t.Helper()
	var timer *scheduleTimer
	scheduleEventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, candidate := range c.timers {
			if candidate.armed {
				timer = candidate
				return true
			}
		}
		return false
	})
	c.mu.Lock()
	c.now = now
	timer.armed = false
	timer.ch <- now
	c.mu.Unlock()
	scheduleEventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return timer.armed })
}

func TestScheduleClockStartsStopsAndKeepsPriority(t *testing.T) {
	base := time.Date(2026, 11, 22, 22, 0, 0, 0, time.UTC)
	clock := &scheduleClock{now: base}
	started := make(chan string, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.URL.Path
		w.Header().Set("Content-Length", "100")
		fmt.Fprint(w, "prefix")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	m := New(Options{Dir: t.TempDir(), MaxConcurrent: 1, Clock: clock})
	defer m.Shutdown()
	plan := Schedule{Enabled: true, StartAt: base.Add(time.Minute), DailyEnabled: true, DailyStartMinute: 1321, DailyEndMinute: 1322}
	ids := []string{}
	for _, name := range []string{"a", "b"} {
		id, err := m.AddTask(AddOptions{URL: srv.URL + "/" + name, Schedule: &plan})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	checkStart := func(want string) {
		t.Helper()
		select {
		case got := <-started:
			if got != want {
				t.Fatalf("start %s want %s", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("start timeout")
		}
	}
	clock.advance(t, base.Add(time.Minute))
	checkStart("/a")
	waitDownloading(t, m, ids[0], 6)
	clock.advance(t, base.Add(2*time.Minute))
	scheduleEventually(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.active == 0 })
	items, _ := m.Snapshot()
	if items[0].Done != 6 || items[0].ScheduleWaiting != "等待每日允许时段" || items[0].RetryCount != 0 || items[0].ManualPaused {
		t.Fatal(items[0])
	}
	clock.advance(t, base.Add(24*time.Hour+time.Minute))
	checkStart("/a")
	clock.advance(t, base.Add(24*time.Hour+time.Minute+time.Second))
	select {
	case extra := <-started:
		t.Fatalf("duplicate worker %s", extra)
	default:
	}
	m.Pause(ids[0])
	checkStart("/b")
	m.PauseAll()
	scheduleEventually(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.active == 0 })
	clock.advance(t, base.Add(48*time.Hour+time.Minute))
	items, _ = m.Snapshot()
	for _, item := range items {
		if !item.ManualPaused || item.State != StatePaused {
			t.Fatal(item)
		}
	}
	select {
	case extra := <-started:
		t.Fatalf("manual pause resumed %s", extra)
	default:
	}
	clock.advance(t, base.Add(48*time.Hour+3*time.Minute))
	m.ResumeAll()
	items, _ = m.Snapshot()
	for _, item := range items {
		if item.ManualPaused || item.State != StatePaused || item.ScheduleWaiting == "" {
			t.Fatal("bulk resume bypassed schedule", item)
		}
	}
	m.Resume(ids[0])
	items, _ = m.Snapshot()
	if items[0].ManualPaused || items[0].ScheduleWaiting == "" {
		t.Fatal("resume bypassed window")
	}
	plan.Enabled = false
	if err := m.SetSchedule(ids[0], plan); err != nil {
		t.Fatal(err)
	}
	clock.advance(t, base.Add(72*time.Hour+time.Minute))
	items, _ = m.Snapshot()
	if items[0].State != StatePaused || !items[0].ManualPaused {
		t.Fatal("disable resumed task")
	}
}

func TestScheduleMissedWindowRollbackRestartAndFailure(t *testing.T) {
	base := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	clock := &scheduleClock{now: base}
	started := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.URL.Path
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	m := New(Options{Dir: t.TempDir(), Clock: clock})
	plan := Schedule{Enabled: true, StartAt: base.Add(time.Hour), DailyEnabled: true, DailyStartMinute: 540, DailyEndMinute: 600}
	id, err := m.AddTask(AddOptions{URL: srv.URL + "/failed", Schedule: &plan})
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(t, base.Add(3*time.Hour)) // Slept through the entire window.
	items, _ := m.Snapshot()
	if !items[0].Schedule.StartReached || items[0].State != StatePaused {
		t.Fatal(items[0])
	}
	clock.advance(t, base.Add(-time.Hour)) // Threshold stays reached after rollback.
	if !m.Records()[0].Schedule.StartReached {
		t.Fatal("threshold reset")
	}
	records := m.Records()
	m.Shutdown()
	clock = &scheduleClock{now: base.Add(25 * time.Hour)} // Missed absolute start; current window is open.
	restored := New(Options{Dir: m.dir, Clock: clock})
	defer restored.Shutdown()
	records = append(records, Record{ID: "legacy", URL: srv.URL + "/legacy", Path: filepath.Join(m.dir, "legacy"), State: StateQueued})
	restored.Restore(records)
	waitState(t, restored, id, StateFailed)
	select {
	case got := <-started:
		if got != "/failed" {
			t.Fatal(got)
		}
	default:
		t.Fatal("missed start not evaluated")
	}
	clock.advance(t, base.Add(49*time.Hour))
	select {
	case got := <-started:
		t.Fatalf("failed/legacy auto resumed: %s", got)
	default:
	}
	restored.Retry(id)
	waitState(t, restored, id, StateFailed)
	select {
	case <-started:
	default:
		t.Fatal("explicit retry ignored")
	}
	secondRecords := restored.Records()
	restored.Shutdown()
	final := New(Options{Dir: m.dir, Clock: &scheduleClock{now: base.Add(73 * time.Hour)}})
	defer final.Shutdown()
	final.Restore(secondRecords)
	got, _ := final.Snapshot()
	if got[0].State != StateFailed || got[1].State != StatePaused {
		t.Fatal(got)
	}
}

func TestScheduleStartupWaitsForPlatformServices(t *testing.T) {
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { started <- struct{}{}; fmt.Fprint(w, "ok") }))
	defer srv.Close()
	m := New(Options{Dir: t.TempDir(), DeferScheduling: true})
	defer m.Shutdown()
	m.Restore([]Record{{ID: "ready", URL: srv.URL + "/ready", Path: filepath.Join(m.dir, "ready"), Schedule: Schedule{Enabled: true}}})
	items, _ := m.Snapshot()
	if items[0].State != StatePaused {
		t.Fatal("startup barrier bypassed")
	}
	m.StartScheduling()
	waitState(t, m, "ready", StateCompleted)
	select {
	case <-started:
	default:
		t.Fatal("ready plan did not start")
	}
}
