package download

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in local measurement, excluded from routine correctness-test timings.
// Run scripts/measure-download.ps1 for isolated Windows measurements.
func TestLocalTransferMeasurement(t *testing.T) {
	if os.Getenv("DOWNLOADER_MEASURE") != "1" {
		t.Skip("set DOWNLOADER_MEASURE=1")
	}
	data := bytes.Repeat([]byte("0123456789abcdef"), 2*1024*1024) // exactly 32 MiB
	for _, scenario := range []struct {
		name        string
		connections int
		limited     bool
	}{{"single-unlimited", 1, false}, {"multi-unlimited", 4, false}, {"single-server-limited", 1, true}, {"multi-server-limited", 4, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			var active, peak, requests atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := active.Add(1)
				defer active.Add(-1)
				requests.Add(1)
				for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
				}
				w.Header().Set("ETag", `"measurement"`)
				if scenario.limited {
					w = &limitedMeasureWriter{ResponseWriter: w, start: time.Now(), request: r}
				}
				http.ServeContent(w, r, "measurement.bin", time.Unix(1700000000, 0), bytes.NewReader(data))
			}))
			defer s.Close()
			m := New(Options{Dir: t.TempDir(), AutoStart: true, MaxConnections: 16})
			defer m.Shutdown()
			cpuStart := measurementCPU()
			const runs = 8
			var elapsed time.Duration
			for i := range runs {
				start := time.Now()
				id, err := m.AddTask(AddOptions{URL: fmt.Sprintf("%s/file-%d", s.URL, i), ConnectionLimit: scenario.connections})
				if err != nil {
					t.Fatal(err)
				}
				it := waitState(t, m, id, StateCompleted)
				elapsed += time.Since(start)
				got, err := os.ReadFile(it.Path)
				if err != nil || !bytes.Equal(got, data) {
					t.Fatal("measurement content mismatch")
				}
				m.Remove(id, true)
			}
			cpuEnd := measurementCPU()
			cpuSeconds := -1.0
			if cpuStart >= 0 && cpuEnd >= 0 {
				cpuSeconds = cpuEnd - cpuStart
			}
			fmt.Printf("MEASURE scenario=%s runs=%d bytes=%d seconds=%.4f MiB_per_s=%.2f peak_requests=%d requests=%d cpu_seconds=%.4f\n", scenario.name, runs, len(data)*runs, elapsed.Seconds(), float64(len(data)*runs)/1024/1024/elapsed.Seconds(), peak.Load(), requests.Load(), cpuSeconds)
		})
	}
}

type limitedMeasureWriter struct {
	http.ResponseWriter
	start   time.Time
	written int64
	request *http.Request
}

func (w *limitedMeasureWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n, err := w.ResponseWriter.Write(p[:min(len(p), 32*1024)])
		total += n
		w.written += int64(n)
		p = p[n:]
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, fmt.Errorf("short write")
		}
		if f, ok := w.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
		delay := time.Until(w.start.Add(time.Duration(float64(w.written) / float64(8*1024*1024) * float64(time.Second))))
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-w.request.Context().Done():
				timer.Stop()
				return total, w.request.Context().Err()
			case <-timer.C:
			}
		}
	}
	return total, nil
}
