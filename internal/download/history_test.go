package download

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func TestHistoryFrequencyGapsZerosAndClockChanges(t *testing.T) {
	base := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	boundary := speedHistory{}
	boundary.sample(base, base, 0, StateDownloading)
	boundary.sample(base.Add(2*time.Second), base.Add(2*time.Second), 200, StateDownloading)
	if boundary.points[1].Gap || boundary.points[1].BytesPerSecond != 100 {
		t.Fatal("exact two-second boundary")
	}
	h := speedHistory{}
	if !h.sample(base, base, 10, StateDownloading) || !h.points[0].Gap {
		t.Fatal("first point invented a speed")
	}
	for i := 1; i < 10; i++ {
		if h.sample(base.Add(time.Duration(i)*100*time.Millisecond), base.Add(time.Duration(i)*100*time.Millisecond), 20, StateDownloading) {
			t.Fatal("sampled more than once/second")
		}
	}
	h.sample(base.Add(time.Second), base.Add(time.Second), 210, StateDownloading)
	if h.points[1].BytesPerSecond != 200 || h.points[1].Gap {
		t.Fatal(h.points[1])
	}
	for i, state := range []State{StateQueued, StatePaused, StateFailed, StateCompleted} {
		at := base.Add(time.Duration(i+2) * time.Second)
		h.sample(at, at, 410+int64(i*100), state)
		if h.points[h.count-1].BytesPerSecond != 0 || h.points[h.count-1].Gap {
			t.Fatal("inactive not zero")
		}
	}
	h.sample(base.Add(8*time.Second), base.Add(8*time.Second), 1000, StateDownloading)
	if !h.points[h.count-1].Gap {
		t.Fatal("long interval interpolated")
	}
	h.sample(base.Add(-time.Hour), base.Add(9*time.Second), 1200, StateDownloading)
	if h.count != 1 || !h.points[0].Gap {
		t.Fatal("rollback left future data")
	}
	for i := 1; i <= 200; i++ {
		h.sample(base.Add(-time.Hour+time.Duration(i)*time.Second), base.Add(time.Duration(9+i)*time.Second), 1200+int64(i*10), StateDownloading)
	}
	if h.count != HistoryCapacity {
		t.Fatal("capacity", h.count)
	}
	h.sample(base.Add(time.Hour), base.Add(210*time.Second), 999999, StateDownloading)
	if h.count != 1 || !h.points[h.head].Gap {
		t.Fatal("old window not expired")
	}
}

func TestHistoryTimerQueryRemoveRestartAndShutdown(t *testing.T) {
	clock := &scheduleClock{now: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)}
	m := New(Options{Dir: t.TempDir(), Clock: clock})
	defer m.Shutdown()
	id := addOne(t, m, "https://example.invalid/paused")
	if len(m.History(id)) != 0 {
		t.Fatal("new task has history")
	}
	clock.advance(t, clock.Now().Add(time.Second))
	clock.advance(t, clock.Now().Add(time.Second))
	history := m.History(id)
	if len(history) != 2 || history[1].Gap || history[1].BytesPerSecond != 0 {
		t.Fatal(history)
	}
	for range 30 {
		m.Snapshot()
		m.History(id)
	}
	if len(m.History(id)) != 2 {
		t.Fatal("queries sampled")
	}
	history[1].BytesPerSecond = 99
	if m.History(id)[1].BytesPerSecond != 0 {
		t.Fatal("query returned mutable buffer")
	}
	records := m.Records()
	encoded, _ := json.Marshal(records)
	if strings.Contains(string(encoded), "history") || strings.Contains(string(encoded), "BytesPerSecond") {
		t.Fatal("history persisted")
	}
	other := New(Options{Dir: m.dir, Clock: &scheduleClock{now: clock.Now()}})
	defer other.Shutdown()
	other.Restore(records)
	if len(other.History(id)) != 0 {
		t.Fatal("restored invented history")
	}
	retained := m.byID[id]
	m.Remove(id, false)
	if retained.history != nil || len(m.History(id)) != 0 {
		t.Fatal("removed task kept ring")
	}
	m.Shutdown()
	clock.mu.Lock()
	defer clock.mu.Unlock()
	for _, timer := range clock.timers {
		if timer.armed {
			t.Fatal("sampler timer still running")
		}
	}
}

func TestHistoryAggregatesReadersAndRetryBytesWithoutDuplicateProgress(t *testing.T) {
	clock := &scheduleClock{now: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)}
	m := New(Options{Dir: t.TempDir(), Clock: clock})
	defer m.Shutdown()
	job := &task{item: Item{ID: "network", State: StateDownloading}, segmentProgress: map[int64]int64{}}
	m.tasks = []*task{job}
	m.byID[job.item.ID] = job
	m.sampleHistories()
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timer := time.NewTimer(time.Hour)
			defer timer.Stop()
			r := networkReader{ctx: context.Background(), manager: m, task: job, body: bytes.NewReader(make([]byte, 100)), timer: timer, stalled: &atomic.Bool{}}
			if n, err := r.Read(make([]byte, 100)); n != 100 || err != nil {
				t.Error(n, err)
			}
		}()
	}
	wg.Wait()
	m.advanceSegment(job, 0, 100)
	m.advanceSegment(job, 100, 100)
	m.advanceSegment(job, 0, 100)
	clock.mu.Lock()
	clock.now = clock.now.Add(time.Second)
	clock.mu.Unlock()
	m.sampleHistories()
	items, _ := m.Snapshot()
	h := m.History("network")
	if items[0].NetworkBytes != 300 || items[0].Done != 200 || len(h) != 2 || h[1].BytesPerSecond != 300 {
		t.Fatal(items, h)
	}
}

func BenchmarkHistorySnapshot(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			m := New(Options{Dir: b.TempDir()})
			defer m.Shutdown()
			now := m.clock.Now()
			for i := 0; i < n; i++ {
				id := fmt.Sprint(i)
				task := &task{item: Item{ID: id, State: StatePaused}, history: &speedHistory{count: HistoryCapacity}}
				for j := range task.history.points {
					task.history.points[j] = SpeedSample{At: now.Add(time.Duration(j-HistoryCapacity+1) * time.Second)}
				}
				m.tasks = append(m.tasks, task)
				m.byID[id] = task
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(n)*float64(unsafe.Sizeof(speedHistory{})), "history-bytes")
			for i := 0; i < b.N; i++ {
				m.Snapshot()
			}
		})
	}
}
