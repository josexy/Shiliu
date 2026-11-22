package download

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// connectionGate grants one request at a time in FIFO order. A worker releases
// its lease before joining the tail again; nobody holds a partial batch.
type connectionGate struct {
	mu            sync.Mutex
	limit, active int
	queue         []*connectionWaiter
}
type connectionWaiter struct {
	ready   chan struct{}
	granted bool
}

func (g *connectionGate) grantLocked() {
	for g.active < g.limit && len(g.queue) > 0 {
		w := g.queue[0]
		g.queue = g.queue[1:]
		g.active++
		w.granted = true
		close(w.ready)
	}
}
func (g *connectionGate) set(n int) { g.mu.Lock(); g.limit = n; g.grantLocked(); g.mu.Unlock() }
func (g *connectionGate) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w := &connectionWaiter{ready: make(chan struct{})}
	g.mu.Lock()
	g.queue = append(g.queue, w)
	g.grantLocked()
	g.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-w.ready:
	}
	g.mu.Lock()
	if err := ctx.Err(); err != nil {
		if w.granted {
			g.active--
		} else {
			for i, v := range g.queue {
				if v == w {
					g.queue = append(g.queue[:i], g.queue[i+1:]...)
					break
				}
			}
		}
		g.grantLocked()
		g.mu.Unlock()
		return nil, err
	}
	g.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { g.mu.Lock(); g.active--; g.grantLocked(); g.mu.Unlock() }) }, nil
}

func (m *Manager) acquireConnection(ctx context.Context, t *task) (func(), error) {
	m.mu.Lock()
	t.item.WaitingConnections++
	m.mu.Unlock()
	m.notify()
	release, err := m.connections.acquire(ctx)
	m.mu.Lock()
	t.item.WaitingConnections--
	if err == nil {
		t.item.ActiveConnections++
	}
	m.mu.Unlock()
	m.notify()
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() { m.mu.Lock(); t.item.ActiveConnections--; m.mu.Unlock(); release(); m.notify() })
	}, nil
}

func validateConnections(n, max int) error {
	if n < 1 || n > max {
		return fmt.Errorf("连接数须为 1–%d 的整数", max)
	}
	return nil
}

func ParseConnectionCount(raw string, maximum int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("连接数须为 1–%d 的整数", maximum)
	}
	return n, validateConnections(n, maximum)
}
func (m *Manager) SetMaxConnections(n int) error {
	if err := validateConnections(n, 64); err != nil {
		return err
	}
	m.connections.set(n)
	return nil
}
func (m *Manager) SetDefaultConnections(n int) error {
	if err := validateConnections(n, 8); err != nil {
		return err
	}
	m.mu.Lock()
	m.defaultConnections = n
	m.mu.Unlock()
	return nil
}

// Per-task changes take effect on its next start. Existing workers retain their
// bounded pool; the global gate can be lowered live without canceling data.
func (m *Manager) SetTaskConnections(id string, n int) error {
	if err := validateConnections(n, 8); err != nil {
		return err
	}
	m.mu.Lock()
	t := m.byID[id]
	if t == nil || t.removed || m.closed {
		m.mu.Unlock()
		return fmt.Errorf("任务不可用")
	}
	t.item.ConnectionLimit = n
	m.mu.Unlock()
	m.notify()
	return nil
}
