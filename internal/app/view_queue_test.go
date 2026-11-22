package app

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
	"go-downloader/internal/settings"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func (f *fakeEngine) MoveQueue(id string, move download.QueueMove) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("MoveQueue:" + id + ":" + string(move))
	return nil
}

func TestQueueUIUsesFullQueueDespiteFilterAndNameSort(t *testing.T) {
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
	m := download.New(download.Options{Dir: t.TempDir(), AutoStart: true, MaxConcurrent: 1})
	defer m.Shutdown()
	m.Add(srv.URL + "/running")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("start timeout")
	}
	m.Add(srv.URL + "/z.zip\n" + srv.URL + "/hidden.zip\n" + srv.URL + "/a.zip")
	items, _ := m.Snapshot()
	a := newTestApp(t, &fakeEngine{})
	a.engine = m
	a.s.Sort = settings.SortName
	a.query = "a.zip"
	a.openDetails(items[3].ID)
	tt := testerFor(a)
	if !tt.HasText("队列位置 3 / 3") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("队列上移"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("队列位置 2 / 3") {
		t.Fatal(tt.Texts())
	}
	// The focused button survives rebuild and handles Enter a second time.
	tt.Key(0, ui.KeyEnter)
	if !tt.HasText("队列位置 1 / 3") {
		t.Fatal(tt.Texts())
	}
	tt.Key(0, ui.KeyEnter) // Now disabled: cannot move past the first item.
	if a.s.Sort != settings.SortName {
		t.Fatal("changed presentation sorting")
	}
	release <- struct{}{}
	select {
	case got := <-started:
		if got != "/a.zip" {
			t.Fatal(got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queue did not start")
	}
}

func TestQueueMenusAndDisabledActions(t *testing.T) {
	for _, view := range viewValues {
		it := seedItem("q", "q.zip", download.StateQueued)
		it.QueuePosition, it.QueueLength = 1, 2
		f := &fakeEngine{items: []download.Item{it}}
		a := newTestApp(t, f)
		a.s.View = view
		tt := testerFor(a)
		if err := tt.RightClick("q.zip"); err != nil {
			t.Fatal(err)
		}
		_ = tt.ChooseMenuItem("队列上移")
		if f.hasCall("MoveQueue:") {
			t.Fatal("disabled action ran")
		}
		tt.Key(0, ui.KeyEscape)
		if err := tt.RightClick("q.zip"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem("队列下移"); err != nil {
			t.Fatal(err)
		}
		if !f.hasCall("MoveQueue:q:down") {
			t.Fatal("menu did not dispatch")
		}
	}
}
