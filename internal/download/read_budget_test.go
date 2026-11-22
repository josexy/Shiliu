package download

import (
	"context"
	"testing"
	"time"
)

func TestJointBudgetAccounting(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		gRate, tRate     int64
		gTokens, tTokens float64
		wantN            int
		gLeft, tLeft     float64
	}{
		{0, 0, 0, 0, 16384, 0, 0},
		{1000, 0, 100, 0, 100, 0, 0},
		{0, 200, 0, 20, 20, 0, 0},
		{1000, 200, 100, 20, 20, 80, 0},
		{1000, 200, 100, 0, 0, 100, 0},
		{1000, 200, 0, 20, 0, 0, 20},
	} {
		global := &bandwidth{rate: tc.gRate, tokens: tc.gTokens, updated: now}
		task := &bandwidth{rate: tc.tRate, tokens: tc.tTokens, updated: now}
		n, _, _, _ := reserveReadBudget(global, task, 16384, now)
		if n != tc.wantN || global.tokens != tc.gLeft || task.tokens != tc.tLeft {
			t.Fatalf("budget %+v: n=%d global=%v task=%v", tc, n, global.tokens, task.tokens)
		}
	}
	global := &bandwidth{rate: 1000, tokens: 100, updated: now}
	task := &bandwidth{rate: 1000, tokens: 100, updated: now}
	n, _, gChanged, tChanged := reserveReadBudget(global, task, 64, now)
	if n != 64 {
		t.Fatal("reservation failed")
	}
	refundReadBudgetAt(global, task, 24, now)
	if global.tokens != 60 || task.tokens != 60 {
		t.Fatal("short read did not refund both buckets")
	}
	for _, changed := range []<-chan struct{}{gChanged, tChanged} {
		select {
		case <-changed:
		default:
			t.Fatal("refund did not wake waiters")
		}
	}
	refundReadBudgetAt(global, task, 1000, now)
	if global.tokens != 100 || task.tokens != 100 {
		t.Fatal("refund exceeded burst capacity")
	}
}

func TestJointBudgetUpdateAndCancel(t *testing.T) {
	global, task := &bandwidth{}, &bandwidth{}
	global.set(1000)
	task.set(1)
	if _, err := acquireReadBudget(context.Background(), global, task, 1); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := acquireReadBudget(context.Background(), global, task, 16*1024); done <- err }()
	// The next task token is a second away. Removing the task limit must wake
	// this call, without waiting for that token or a global-setting update.
	time.Sleep(20 * time.Millisecond)
	task.set(0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("rate update did not wake joint waiter")
	}
	global.set(1)
	task.set(1)
	global.mu.Lock()
	global.tokens = 0
	global.mu.Unlock()
	task.mu.Lock()
	task.tokens = 0
	task.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, err := acquireReadBudget(ctx, global, task, 16384); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("joint budget did not cancel")
	}
}
