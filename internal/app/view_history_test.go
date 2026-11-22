package app

import (
	"testing"
	"time"

	"go-downloader/internal/download"
)

func (f *fakeEngine) History(id string) []download.SpeedSample {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]download.SpeedSample(nil), f.histories[id]...)
}

func TestHistoryDetailsUpdateReopenAndRemove(t *testing.T) {
	it := seedItem("history", "file.bin", download.StateDownloading)
	f := &fakeEngine{items: []download.Item{it}, histories: map[string][]download.SpeedSample{}}
	a := newTestApp(t, f)
	a.openDetails(it.ID)
	tt := testerFor(a)
	if !tt.HasText("等待采样") {
		t.Fatal(tt.Texts())
	}
	f.histories[it.ID] = []download.SpeedSample{{At: testNow.Add(-time.Second), BytesPerSecond: 1024}, {At: testNow, BytesPerSecond: 2048}}
	tt.Frame()
	if !tt.HasText("当前 2.00 KB/s") || !tt.HasText("峰值 2.00 KB/s") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("关闭任务详情"); err != nil {
		t.Fatal(err)
	}
	a.openDetails(it.ID)
	tt.Frame()
	if !tt.HasText("有效 2 点") {
		t.Fatal("reopen lost history")
	}
	f.items[0].State = download.StatePaused
	tt.Frame()
	if !tt.HasText("当前 0 B/s") {
		t.Fatal("paused speed not zero")
	}
	f.items = nil
	delete(f.histories, it.ID)
	tt.Frame()
	if !tt.HasText("此任务已从列表移除") || tt.HasText("峰值") {
		t.Fatal("removed task kept history view")
	}
}

func TestHistoryPlotDoesNotBridgeGaps(t *testing.T) {
	samples := []download.SpeedSample{{At: testNow.Add(-6 * time.Second), BytesPerSecond: 10}, {At: testNow.Add(-5 * time.Second), BytesPerSecond: 20}, {At: testNow.Add(-4 * time.Second), Gap: true}, {At: testNow.Add(-time.Second), BytesPerSecond: 30}, {At: testNow, BytesPerSecond: 40}}
	lines := historyLines(samples, testNow, 40)
	if len(lines) != 2 || len(lines[0]) != 2 || len(lines[1]) != 2 {
		t.Fatal("interpolated missing interval", lines)
	}
	copy := (&fakeEngine{histories: map[string][]download.SpeedSample{"x": samples}}).History("x")
	copy[0].BytesPerSecond = 999
	if samples[0].BytesPerSecond != 10 {
		t.Fatal("paint input mutable")
	}
}

func historyFixture(state string) []download.SpeedSample {
	if state == "empty" {
		return nil
	}
	var samples []download.SpeedSample
	for i := 0; i < 120; i++ {
		if state == "gap" && i >= 30 && i <= 55 {
			continue
		}
		value := float64(256*1024 + (i%9)*64*1024)
		if (state == "paused" || state == "completed") && i >= 90 {
			value = 0
		}
		if state == "peak" && i == 75 {
			value = 12 * 1024 * 1024
		}
		samples = append(samples, download.SpeedSample{At: testNow.Add(time.Duration(i-119) * time.Second), BytesPerSecond: value, Gap: i == 0 || state == "gap" && i == 56})
	}
	return samples
}
