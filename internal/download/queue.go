package download

import (
	"fmt"
	"math"
	"sort"
)

type QueueMove string

const (
	QueueFront QueueMove = "front"
	QueueUp    QueueMove = "up"
	QueueDown  QueueMove = "down"
)

// MoveQueue adjusts only waiting tasks. Paused tasks keep their order anchors;
// workers already holding a slot are never preempted. Recheck under the lock
// because a menu's snapshot can be stale by the time its action is delivered.
func (m *Manager) MoveQueue(id string, move QueueMove) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("下载器正在退出")
	}
	queue := m.queueLocked()
	from := -1
	for i, t := range queue {
		if t.item.ID == id {
			from = i
			break
		}
	}
	if from < 0 {
		m.mu.Unlock()
		return fmt.Errorf("任务已开始、暂停或移除，无法调整队列")
	}
	to := from
	switch move {
	case QueueFront:
		to = 0
	case QueueUp:
		to = max(0, from-1)
	case QueueDown:
		to = min(len(queue)-1, from+1)
	default:
		m.mu.Unlock()
		return fmt.Errorf("无效的队列操作")
	}
	if to == from {
		m.mu.Unlock()
		return nil
	}
	slots := make([]int64, len(queue))
	for i, t := range queue {
		slots[i] = t.queueOrder
	}
	target := queue[from]
	if to < from {
		copy(queue[to+1:from+1], queue[to:from])
	} else {
		copy(queue[from:to], queue[from+1:to+1])
	}
	queue[to] = target
	for i, t := range queue {
		t.queueOrder = slots[i]
	}
	m.mu.Unlock()
	m.notify()
	return nil
}

// normalizeQueueLocked repairs persisted orders without changing display order.
// Positive orders come first (ties retain record order); legacy/invalid records
// follow in record order. All-legacy files therefore preserve their old order.
func (m *Manager) normalizeQueueLocked() {
	ordered := append([]*task(nil), m.tasks...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i].queueOrder, ordered[j].queueOrder
		if a <= 0 || b <= 0 {
			return a > 0 && b <= 0
		}
		return a < b
	})
	for i, t := range ordered {
		t.queueOrder = int64(i + 1)
	}
}

func (m *Manager) nextQueueOrderLocked() int64 {
	var last int64
	for _, t := range m.tasks {
		last = max(last, t.queueOrder)
	}
	if last == math.MaxInt64 {
		m.normalizeQueueLocked()
		last = int64(len(m.tasks))
	}
	return last + 1
}

func (m *Manager) queueLocked() []*task {
	var queue []*task
	for _, t := range m.tasks {
		if t.item.State == StateQueued && !t.working {
			queue = append(queue, t)
		}
	}
	sort.SliceStable(queue, func(i, j int) bool { return queue[i].queueOrder < queue[j].queueOrder })
	return queue
}
