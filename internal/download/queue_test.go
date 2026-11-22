package download

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestQueueRestoreRepairsOrder(t *testing.T) {
	for _, tc := range []struct{ orders, want []int64 }{
		{[]int64{0, 0, 0}, []int64{1, 2, 3}},
		{[]int64{3, 1, 2}, []int64{3, 1, 2}},
		{[]int64{4, 4, 0, -2, 1}, []int64{2, 3, 4, 5, 1}},
	} {
		m := New(Options{Dir: t.TempDir()})
		var records []Record
		for i, order := range tc.orders {
			id := fmt.Sprint(i)
			records = append(records, Record{ID: id, URL: "https://example.com/" + id, Path: filepath.Join(m.dir, id), State: StateQueued, QueueOrder: order})
		}
		m.Restore(records)
		var got []int64
		for _, r := range m.Records() {
			got = append(got, r.QueueOrder)
			if r.State != StatePaused {
				t.Fatal("restore started a task")
			}
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("orders=%v, want %v", got, tc.want)
		}
		m.Shutdown()
	}
}

func TestQueueMovesKeepPausedAnchorsAndNewItemsAtTail(t *testing.T) {
	m := New(Options{Dir: t.TempDir(), AutoStart: true})
	defer m.Shutdown()
	m.max = 0                              // Keep the queue waiting so each boundary is deterministic.
	m.SetOnChange(func() { m.Snapshot() }) // Would deadlock if notified under lock.
	ids := []string{}
	for _, name := range []string{"a", "b", "c", "d"} {
		ids = append(ids, addOne(t, m, "https://example.com/"+name))
	}
	assertOrder := func(want ...string) {
		t.Helper()
		m.mu.Lock()
		queue := m.queueLocked()
		var got []string
		for _, task := range queue {
			got = append(got, task.item.ID)
		}
		m.mu.Unlock()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("queue %v want %v", got, want)
		}
	}
	for _, move := range []struct {
		id string
		op QueueMove
	}{{ids[0], QueueUp}, {ids[3], QueueDown}, {ids[2], QueueFront}} {
		if err := m.MoveQueue(move.id, move.op); err != nil {
			t.Fatal(err)
		}
	}
	assertOrder(ids[2], ids[0], ids[1], ids[3])
	m.Pause(ids[0])
	if err := m.MoveQueue(ids[1], QueueUp); err != nil {
		t.Fatal(err)
	}
	m.Resume(ids[0])
	assertOrder(ids[1], ids[0], ids[2], ids[3])
	m.Remove(ids[2], false)
	newID := addOne(t, m, "https://example.com/e")
	assertOrder(ids[1], ids[0], ids[3], newID)
	items, _ := m.Snapshot()
	for _, it := range items {
		if it.QueuePosition < 1 || it.QueueLength != 4 {
			t.Fatalf("position: %+v", it)
		}
	}
	m.mu.Lock()
	m.byID[ids[0]].working = true
	m.mu.Unlock()
	if err := m.MoveQueue(ids[0], QueueFront); err == nil {
		t.Fatal("moved started worker")
	}
	m.mu.Lock()
	m.byID[ids[0]].working = false
	m.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = m.MoveQueue(newID, QueueFront) }()
	go func() { defer wg.Done(); m.Remove(newID, false) }()
	wg.Wait()
	assertOrder(ids[1], ids[0], ids[3])
	if err := m.MoveQueue(newID, QueueFront); err == nil {
		t.Fatal("moved removed task")
	}
}

func TestQueueReorderDoesNotPreemptWorkers(t *testing.T) {
	started := make(chan string, 5)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.URL.Path
		select {
		case <-release:
			fmt.Fprint(w, "ok")
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	m := newTestManager(t, t.TempDir(), 2)
	defer m.Shutdown()
	ids := []string{}
	for _, name := range []string{"a", "b", "c", "d"} {
		ids = append(ids, addOne(t, m, srv.URL+"/"+name))
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("start timeout")
		}
	}
	if err := m.MoveQueue(ids[3], QueueFront); err != nil {
		t.Fatal(err)
	}
	if err := m.MoveQueue(ids[0], QueueDown); err == nil {
		t.Fatal("accepted running task")
	}
	_, sum := m.Snapshot()
	if sum.Active-sum.Queued != 2 {
		t.Fatal(sum)
	}
	release <- struct{}{}
	select {
	case got := <-started:
		if got != "/d" {
			t.Fatal(got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("next start timeout")
	}
}

func TestQueueSchedulingUsesPersistedOrder(t *testing.T) {
	started := make(chan string, 4)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.URL.Path
		select {
		case <-release:
			fmt.Fprint(w, "ok")
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	m := newTestManager(t, t.TempDir(), 1)
	defer m.Shutdown()
	var records []Record
	for i, order := range []int64{3, 1, 2} {
		id := fmt.Sprint(i)
		records = append(records, Record{ID: id, URL: srv.URL + "/" + id, Path: filepath.Join(m.dir, id), QueueOrder: order})
	}
	m.Restore(records)
	m.ResumeAll()
	for _, want := range []string{"/1", "/2", "/0"} {
		select {
		case got := <-started:
			if got != want {
				t.Fatalf("started %s, want %s", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("start timeout")
		}
		_, sum := m.Snapshot()
		if sum.Active-sum.Queued != 1 {
			t.Fatalf("concurrent workers: %+v", sum)
		}
		select {
		case extra := <-started:
			t.Fatalf("exceeded concurrency: %s", extra)
		default:
		}
		release <- struct{}{}
	}
	waitState(t, m, "0", StateCompleted)
}
