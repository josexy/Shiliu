package diagnostics

import "sync"

type Buffer struct {
	mu                 sync.Mutex
	events             [MaxEvents]Event
	head, count, bytes int
	dropped            uint64
}
type Snapshot struct {
	Dropped uint64  `json:"dropped"`
	Events  []Entry `json:"events"`
}

func (b *Buffer) Stats() (count int, dropped uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count, b.dropped
}

func (b *Buffer) Record(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if event.size <= 0 || event.size > MaxEventBytes {
		b.dropped++
		return
	}
	for b.count > 0 && (b.count == MaxEvents || b.bytes+event.size+2 > MaxBytes) {
		b.bytes -= b.events[b.head].size + 1
		b.events[b.head] = Event{}
		b.head = (b.head + 1) % MaxEvents
		b.count--
		b.dropped++
	}
	b.events[(b.head+b.count)%MaxEvents] = event
	b.count++
	b.bytes += event.size + 1
}
func (b *Buffer) Omit(count uint64) { b.mu.Lock(); b.dropped += count; b.mu.Unlock() }
func (b *Buffer) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := Snapshot{Dropped: b.dropped, Events: make([]Entry, b.count)}
	for i := range s.Events {
		s.Events[i] = b.events[(b.head+i)%MaxEvents].entry
	}
	return s
}

// Drain transfers a bounded batch to an external Sink outside its caller's
// lock. The same bounds apply while a burst awaits delivery.
func (b *Buffer) Drain() ([]Event, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	events := make([]Event, b.count)
	for i := range events {
		pos := (b.head + i) % MaxEvents
		events[i] = b.events[pos]
		b.events[pos] = Event{}
	}
	dropped := b.dropped
	b.head, b.count, b.bytes, b.dropped = 0, 0, 0, 0
	return events, dropped
}
