package download

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskRateModelValidationAndRestore(t *testing.T) {
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	id, err := m.AddTask(AddOptions{URL: "example.com/rate", TaskRateLimit: 128 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	for _, rate := range []int64{-1, MaxTaskRateLimit + 1} {
		if err := m.SetTaskRateLimit(id, rate); err == nil {
			t.Fatal("invalid update accepted")
		}
		if _, err := m.AddTask(AddOptions{URL: "example.com/invalid", TaskRateLimit: rate}); err == nil {
			t.Fatal("invalid new task accepted")
		}
	}
	items, _ := m.Snapshot()
	if items[0].TaskRateLimit != 128*1024 {
		t.Fatal("invalid update replaced limit")
	}
	data, err := json.Marshal(m.Records())
	if err != nil {
		t.Fatal(err)
	}
	var records []Record
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	copy := New(Options{Dir: t.TempDir()})
	defer copy.Shutdown()
	copy.Restore(records)
	items, _ = copy.Snapshot()
	if len(items) != 1 || items[0].TaskRateLimit != 128*1024 {
		t.Fatal("rate not restored")
	}
	if err := copy.SetTaskRateLimit(id, 0); err != nil {
		t.Fatal(err)
	}
	if copy.Records()[0].TaskRateLimit != 0 {
		t.Fatal("zero did not remove limit")
	}
	records[0].ID = "old"
	records[0].TaskRateLimit = 0
	copy.Restore(records)
	items, _ = copy.Snapshot()
	if items[1].TaskRateLimit != 0 {
		t.Fatal("legacy task gained a limit")
	}
	if err := m.SetTaskRateLimit("missing", 1); err == nil {
		t.Fatal("missing task accepted")
	}
}

func TestTaskRateKiBConversion(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
		bad  bool
	}{
		{"0", 0, false}, {" 128 ", 131072, false}, {"0.5", 512, false}, {"1048576", MaxTaskRateLimit, false},
		{"-1", 0, true}, {"NaN", 0, true}, {"Inf", 0, true}, {"abc", 0, true}, {"1048577", 0, true}, {"", 0, true},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := ParseTaskRateKiB(tc.raw)
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("got %d %v", got, err)
			}
		})
	}
}

func TestTaskAndGlobalRateLimitsShareNetworkBudget(t *testing.T) {
	content := bytes.Repeat([]byte("content"), 64*1024)
	srv := rangeServer(t, content, len(content), 0, false)
	defer srv.Close()
	m := New(Options{Dir: t.TempDir(), AutoStart: true, MaxConcurrent: 2, RateLimit: 32 * 1024, StallTimeout: 60 * time.Millisecond})
	defer m.Shutdown()
	start := time.Now()
	slow, err := m.AddTask(AddOptions{URL: srv.URL + "/slow", TaskRateLimit: 8 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	fast := addOne(t, m, srv.URL+"/fast")
	time.Sleep(2300 * time.Millisecond)
	items, _ := m.Snapshot()
	slowItem, _ := itemByID(items, slow)
	fastItem, _ := itemByID(items, fast)
	elapsed := time.Since(start).Seconds()
	if float64(slowItem.Done) > 8192*elapsed+819+16384 {
		t.Fatalf("task cap exceeded: %d", slowItem.Done)
	}
	if float64(slowItem.Done+fastItem.Done) > 32768*elapsed+3276+2*16384 {
		t.Fatal("global cap exceeded")
	}
	if fastItem.Done < 16*1024 || fastItem.Done <= slowItem.Done {
		t.Fatalf("slow task retained shared tokens: slow=%d fast=%d", slowItem.Done, fastItem.Done)
	}
	if slowItem.RetryCount != 0 || fastItem.RetryCount != 0 {
		t.Fatal("throttle triggered stall retry")
	}
	m.SetRateLimit(0)
	if err := m.SetTaskRateLimit(slow, 0); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{slow, fast} {
		it := waitState(t, m, id, StateCompleted)
		data, err := os.ReadFile(it.Path)
		if err != nil || !bytes.Equal(data, content) {
			t.Fatal("two-level throttling changed content")
		}
	}
}

func TestDynamicTaskRateDoesNotReconnect(t *testing.T) {
	content := bytes.Repeat([]byte("x"), 1024*1024)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.Write(content) }))
	defer srv.Close()
	m := New(Options{Dir: t.TempDir(), AutoStart: true})
	defer m.Shutdown()
	id, err := m.AddTask(AddOptions{URL: srv.URL + "/dynamic", TaskRateLimit: 128 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	before := waitDownloading(t, m, id, 16*1024).Done
	if err := m.SetTaskRateLimit(id, 1024); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	items, _ := m.Snapshot()
	it, _ := itemByID(items, id)
	if it.Done-before > 16*1024+1024 {
		t.Fatal("lower task limit did not take effect")
	}
	if err := m.SetTaskRateLimit(id, 256*1024); err != nil {
		t.Fatal(err)
	}
	if err := m.SetTaskRateLimit(id, 0); err != nil {
		t.Fatal(err)
	}
	it = waitState(t, m, id, StateCompleted)
	data, err := os.ReadFile(it.Path)
	if err != nil || !bytes.Equal(data, content) || requests.Load() != 1 {
		t.Fatalf("dynamic task rate changed connection/content: %v requests=%d", err, requests.Load())
	}
}

func TestOneByteTaskRateCancellation(t *testing.T) {
	srv := rangeServer(t, bytes.Repeat([]byte("x"), 1024), 1024, 0, false)
	defer srv.Close()
	for _, action := range []string{"pause", "remove", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			m := New(Options{Dir: t.TempDir(), AutoStart: true, StallTimeout: 30 * time.Millisecond})
			defer m.Shutdown()
			id, err := m.AddTask(AddOptions{URL: srv.URL + "/" + action, TaskRateLimit: 1})
			if err != nil {
				t.Fatal(err)
			}
			waitDownloading(t, m, id, 1)
			time.Sleep(80 * time.Millisecond)
			m.mu.Lock()
			task := m.byID[id]
			done := task.done
			retries := task.item.RetryCount
			m.mu.Unlock()
			if retries != 0 {
				t.Fatal("healthy one-byte task was retried")
			}
			start := time.Now()
			switch action {
			case "pause":
				m.Pause(id)
			case "remove":
				m.Remove(id, false)
			case "shutdown":
				m.Shutdown()
			}
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("limit wait did not cancel")
			}
			if time.Since(start) > 500*time.Millisecond {
				t.Fatal("cancellation exceeded 500ms")
			}
			if action != "remove" {
				items, _ := m.Snapshot()
				if items[0].Done != 1 || items[0].State != StatePaused {
					t.Fatal("cancel lost progress")
				}
			}
		})
	}
}
