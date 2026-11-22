package download

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

// networkReader shares download budgets with probes and future parallel
// readers. Only an actual network Read runs the stall timer, never quota waits.
type networkReader struct {
	ctx     context.Context
	manager *Manager
	task    *task
	body    io.Reader
	timer   *time.Timer
	stalled *atomic.Bool
}

func (r networkReader) Read(p []byte) (int, error) {
	if r.stalled.Load() {
		return 0, errStalled
	}
	n, err := acquireReadBudget(r.ctx, &r.manager.bandwidth, &r.task.bandwidth, len(p))
	if err != nil {
		return 0, err
	}
	r.timer.Reset(r.manager.stall)
	read, err := r.body.Read(p[:n])
	r.timer.Stop()
	refundReadBudget(&r.manager.bandwidth, &r.task.bandwidth, n-read)
	if read > 0 {
		r.manager.recordNetworkBytes(r.task, read)
	}
	if r.stalled.Load() {
		return read, errStalled
	}
	if r.ctx.Err() != nil {
		return read, r.ctx.Err()
	}
	return read, err
}
