package download

import (
	"context"
	"sync"
	"time"
)

// bandwidth shares a small token bucket across all transfers. Settings changes
// wake waiters; no manager lock is held while waiting for network bandwidth.
type bandwidth struct {
	mu      sync.Mutex
	rate    int64
	tokens  float64
	updated time.Time
	changed chan struct{}
}

func (b *bandwidth) capacity() int {
	return int(max(1, min(16*1024, b.rate/10)))
}

func (b *bandwidth) refill(now time.Time) {
	if !b.updated.IsZero() && b.rate > 0 {
		b.tokens = min(float64(b.capacity()), b.tokens+now.Sub(b.updated).Seconds()*float64(b.rate))
	}
	b.updated = now
}

func (b *bandwidth) set(rate int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill(time.Now())
	previous := b.rate
	b.rate = max(0, rate)
	if previous == 0 {
		b.tokens = float64(b.capacity())
	} else {
		b.tokens = min(b.tokens, float64(b.capacity()))
	}
	if b.changed != nil {
		close(b.changed)
	}
	b.changed = make(chan struct{})
}

// take reserves bytes before reading. A short read refunds unused tokens.
func (b *bandwidth) take(ctx context.Context, want int) (int, error) {
	return acquireReadBudget(ctx, b, nil, want)
}

func (b *bandwidth) refund(n int) {
	refundReadBudget(b, nil, n)
}

// SetRateLimit changes the total bytes per second allowed for all downloads.
// Zero or a negative value removes the limit, including for running transfers.
func (m *Manager) SetRateLimit(bytesPerSecond int64) {
	m.bandwidth.set(bytesPerSecond)
}
