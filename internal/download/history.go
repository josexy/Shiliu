package download

import (
	"math"
	"time"
)

const HistoryCapacity = 120
const HistoryWindow = 120 * time.Second

type SpeedSample struct {
	At             time.Time
	BytesPerSecond float64
	Gap            bool // No trustworthy interval; never interpolate across this point.
}
type speedHistory struct {
	points             [HistoryCapacity]SpeedSample
	head, count        int
	lastWall, lastMono time.Time
	lastBytes          int64
}

// Real elapsed time uses Go's monotonic reading. Now separately carries the
// current system zone for schedules and labels; location conversion strips a
// time.Time's monotonic component. Fake clocks may provide both readings.
type monotonicClock interface{ MonotonicNow() time.Time }

func (realClock) MonotonicNow() time.Time { return time.Now() }

func (h *speedHistory) sample(wall, mono time.Time, networkBytes int64, state State) bool {
	first := h.count == 0
	elapsed := mono.Sub(h.lastMono)
	if !first && elapsed >= 0 && elapsed < time.Second {
		return false
	}
	gap := first || elapsed <= 0 || elapsed > 2*time.Second || networkBytes < h.lastBytes || networkBytes < 0 || h.lastBytes < 0
	if !first {
		wallElapsed := wall.Sub(h.lastWall)
		if wallElapsed <= 0 || math.Abs(float64(wallElapsed-elapsed)) > float64(500*time.Millisecond) {
			gap = true
		}
		if wall.Before(h.lastWall) {
			h.head, h.count = 0, 0
		}
	}
	point := SpeedSample{At: wall, Gap: gap}
	if !gap && (state == StateDownloading || state == StateConnecting) {
		point.BytesPerSecond = float64(networkBytes-h.lastBytes) / elapsed.Seconds()
	}
	h.lastWall, h.lastMono, h.lastBytes = wall, mono, networkBytes
	cutoff := wall.Add(-HistoryWindow)
	for h.count > 0 && (h.count == HistoryCapacity || h.points[h.head].At.Before(cutoff)) {
		h.points[h.head] = SpeedSample{}
		h.head = (h.head + 1) % HistoryCapacity
		h.count--
	}
	h.points[(h.head+h.count)%HistoryCapacity] = point
	h.count++
	return true
}

func (m *Manager) sampleHistories() {
	wall := m.clock.Now()
	mono := wall
	if clock, ok := m.clock.(monotonicClock); ok {
		mono = clock.MonotonicNow()
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	changed := false
	for _, t := range m.tasks {
		if t.history == nil {
			t.history = &speedHistory{}
		}
		changed = t.history.sample(wall, mono, t.item.NetworkBytes, t.item.State) || changed
	}
	m.mu.Unlock()
	if changed {
		m.notifyThrottled()
	}
}

// History copies only the selected task's bounded history. Snapshot and Record
// never include these arrays, and reading history does not take a new sample.
func (m *Manager) History(id string) []SpeedSample {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.byID[id]
	if t == nil || t.history == nil {
		return nil
	}
	now := m.clock.Now()
	cutoff := now.Add(-HistoryWindow)
	h := t.history
	out := make([]SpeedSample, 0, h.count)
	for i := 0; i < h.count; i++ {
		p := h.points[(h.head+i)%HistoryCapacity]
		if !p.At.Before(cutoff) && !p.At.After(now) {
			out = append(out, p)
		}
	}
	return out
}

func (m *Manager) recordNetworkBytes(t *task, n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	t.item.NetworkBytes += int64(n)
	m.sampleSpeedLocked(t)
	m.mu.Unlock()
	m.notifyThrottled()
}
