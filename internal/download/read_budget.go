package download

import (
	"context"
	"time"
)

// Every task's readers share its bucket and the manager's global bucket. Both
// are locked in global-then-task order, and charged together only when both can
// admit a read. A slow task therefore cannot hold global tokens while waiting.
func acquireReadBudget(ctx context.Context, global, task *bandwidth, want int) (int, error) {
	if want <= 0 {
		return 0, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n, wait, globalChanged, taskChanged := reserveReadBudget(global, task, want, time.Now())
		if n > 0 {
			return n, nil
		}
		timer := time.NewTimer(max(time.Millisecond, wait))
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, ctx.Err()
		case <-globalChanged:
			timer.Stop()
		case <-taskChanged:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func reserveReadBudget(global, task *bandwidth, want int, now time.Time) (int, time.Duration, <-chan struct{}, <-chan struct{}) {
	global.mu.Lock()
	defer global.mu.Unlock()
	if task != nil {
		task.mu.Lock()
		defer task.mu.Unlock()
	}
	n := min(want, 16*1024)
	for _, b := range []*bandwidth{global, task} {
		if b == nil {
			continue
		}
		if b.changed == nil {
			b.changed = make(chan struct{})
		}
		b.refill(now)
		if b.rate > 0 {
			n = min(n, b.capacity())
		}
	}
	var wait time.Duration
	var taskChanged <-chan struct{}
	if task != nil {
		taskChanged = task.changed
	}
	for _, b := range []*bandwidth{global, task} {
		if b != nil && b.rate > 0 && b.tokens < float64(n) {
			wait = max(wait, time.Duration((float64(n)-b.tokens)/float64(b.rate)*float64(time.Second)))
			// Even sub-nanosecond deficits must not grant an unfunded read.
			wait = max(wait, time.Nanosecond)
		}
	}
	if wait > 0 {
		return 0, wait, global.changed, taskChanged
	}
	for _, b := range []*bandwidth{global, task} {
		if b != nil && b.rate > 0 {
			b.tokens -= float64(n)
		}
	}
	return n, 0, global.changed, taskChanged
}

func refundReadBudget(global, task *bandwidth, n int) {
	refundReadBudgetAt(global, task, n, time.Now())
}

func refundReadBudgetAt(global, task *bandwidth, n int, now time.Time) {
	if n <= 0 {
		return
	}
	global.mu.Lock()
	defer global.mu.Unlock()
	if task != nil {
		task.mu.Lock()
		defer task.mu.Unlock()
	}
	for _, b := range []*bandwidth{global, task} {
		if b == nil || b.rate == 0 {
			continue
		}
		b.refill(now)
		b.tokens = min(float64(b.capacity()), b.tokens+float64(n))
		if b.changed != nil {
			close(b.changed)
		}
		b.changed = make(chan struct{})
	}
}
