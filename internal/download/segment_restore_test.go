package download

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

func seedSegments(t *testing.T, m *Manager, url string, data []byte) (string, string, partMeta) {
	t.Helper()
	id, err := m.AddTask(AddOptions{URL: url, ConnectionLimit: 3, FileName: "fixed.bin"})
	if err != nil {
		t.Fatal(err)
	}
	path := m.Records()[0].Path
	meta := partMeta{SchemaVersion: 2, Mode: "segments", URL: url, ResourceURL: url, ETag: `"stable"`, Total: int64(len(data)), Resumable: true, Completed: []byteRange{{0, segmentSize - 1}, {2 * segmentSize, int64(len(data)) - 1}}}
	f, err := os.Create(path + ".part.segments")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(int64(len(data))); err != nil {
		t.Fatal(err)
	}
	for _, r := range meta.Completed {
		if _, err = f.WriteAt(data[r.Start:r.End+1], r.Start); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = writePartMeta(path+".part.meta", meta); err != nil {
		t.Fatal(err)
	}
	return id, path, meta
}

func TestSegmentsRestoreOnlyCommittedRangesAndSourceUpdate(t *testing.T) {
	data := segmentData()
	for _, update := range []bool{false, true} {
		t.Run(fmt.Sprint(update), func(t *testing.T) {
			var mu sync.Mutex
			var ranges []string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				ranges = append(ranges, r.Header.Get("Range"))
				mu.Unlock()
				serveSegmentData(w, r, data)
			}))
			defer s.Close()
			m := New(Options{Dir: t.TempDir()})
			id, path, meta := seedSegments(t, m, s.URL+"/file?v=old", data)
			records := m.Records()
			m.Shutdown()
			restored := New(Options{Dir: t.TempDir()})
			defer restored.Shutdown()
			restored.Restore(records)
			items, _ := restored.Snapshot()
			if items[0].Done != completedBytes(meta) || items[0].State != StatePaused {
				t.Fatal("counted a sparse hole as progress")
			}
			if update {
				d, err := restored.BeginSourceUpdate(id, SourceOptions{URL: s.URL + "/file?v=new"})
				if err != nil {
					t.Fatal(err)
				}
				preview, err := restored.ProbeSource(context.Background(), d)
				if err != nil || !preview.Reuse || preview.Bytes != completedBytes(meta) {
					t.Fatalf("source probe: %+v %v", preview, err)
				}
				if err = restored.CommitSource(d); err != nil {
					t.Fatal(err)
				}
				// Simulate crash before records save; only the committed sidecar knows the URL.
				restored.Shutdown()
				restored = New(Options{Dir: t.TempDir()})
				defer restored.Shutdown()
				restored.Restore(records)
				if restored.Records()[0].URL != s.URL+"/file?v=new" {
					t.Fatal("lost sparse source revision")
				}
			}
			mu.Lock()
			ranges = nil
			mu.Unlock()
			restored.Resume(id)
			it := waitState(t, restored, id, StateCompleted)
			got, _ := os.ReadFile(path)
			if !bytes.Equal(got, data) || it.Path != path {
				t.Fatal("sparse recovery content mismatch")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(ranges) != 2 || ranges[0] != "bytes=0-0" || ranges[1] != fmt.Sprintf("bytes=%d-%d", segmentSize, 2*segmentSize-1) {
				t.Fatalf("downloaded committed ranges: %v", ranges)
			}
		})
	}
}

func TestSegmentsSourceMismatchAndResetRecovery(t *testing.T) {
	data := segmentData()
	changed := bytes.Clone(data)
	changed[len(changed)-1] ^= 0xff
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") == "new" {
			serveSegmentData(w, r, changed)
		} else {
			serveSegmentData(w, r, data)
		}
	}))
	defer s.Close()
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	id, path, _ := seedSegments(t, m, s.URL+"/file?v=old", data)
	old := m.Records()
	d, err := m.BeginSourceUpdate(id, SourceOptions{URL: s.URL + "/file?v=new"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.ProbeSource(context.Background(), d)
	if err != nil || p.Reuse {
		t.Fatalf("reused mismatched tail: %+v %v", p, err)
	}
	if err = m.CommitSource(d); err != nil {
		t.Fatal(err)
	}
	m.Shutdown()
	restored := New(Options{Dir: t.TempDir()})
	defer restored.Shutdown()
	restored.Restore(old)
	if !restored.Records()[0].SourceReset || restored.Records()[0].Done != 0 {
		t.Fatal("reset not recovered")
	}
	restored.Resume(id)
	waitState(t, restored, id, StateCompleted)
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, changed) {
		t.Fatal("old sparse bytes mixed into new source")
	}
}

func TestSegmentsCorruptMetadataAndLegacyPrefix(t *testing.T) {
	data := segmentData()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveSegmentData(w, r, data) }))
	defer s.Close()
	for _, mode := range []string{"corrupt", "unknown", "missing", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			m := New(Options{Dir: t.TempDir()})
			defer m.Shutdown()
			var id, path string
			if mode == "legacy" {
				id, path = sourcePartial(t, m, s.URL+"/file", data, 4000, `"stable"`)
				m.SetTaskConnections(id, 4)
			} else {
				var meta partMeta
				id, path, meta = seedSegments(t, m, s.URL+"/file", data)
				switch mode {
				case "corrupt":
					os.WriteFile(path+".part.meta", []byte("{"), 0600)
				case "missing":
					os.Remove(path + ".part.meta")
				case "unknown":
					meta.SchemaVersion = 99
					writePartMeta(path+".part.meta", meta)
				}
			}
			records := m.Records()
			m.Shutdown()
			restored := New(Options{Dir: t.TempDir()})
			defer restored.Shutdown()
			restored.Restore(records)
			if mode != "legacy" && restored.Records()[0].Done != 0 {
				t.Fatal("invalid metadata reused")
			}
			restored.Resume(id)
			it := waitState(t, restored, id, StateCompleted)
			got, _ := os.ReadFile(path)
			if !bytes.Equal(got, data) || it.TransferMode != "single" || it.FallbackReason == "" {
				t.Fatalf("invalid fallback: %+v", it)
			}
		})
	}
}

func TestConnectionConfigurationAndSidecarReservations(t *testing.T) {
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	id := addOne(t, m, "https://example.com/file")
	if m.Records()[0].ConnectionLimit != 1 || m.connections.limit != 16 {
		t.Fatal("defaults changed")
	}
	for _, n := range []int{0, -1, 9, 100} {
		if err := m.SetTaskConnections(id, n); err == nil {
			t.Fatal("invalid task connection accepted")
		}
	}
	for _, n := range []int{0, -1, 65} {
		if err := m.SetMaxConnections(n); err == nil {
			t.Fatal("invalid global connection accepted")
		}
	}
	for _, n := range []int{1, 8} {
		if err := m.SetTaskConnections(id, n); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int{1, 64} {
		if err := m.SetMaxConnections(n); err != nil {
			t.Fatal(err)
		}
	}
	path := m.Records()[0].Path
	for _, suffix := range []string{".part", ".part.meta", ".part.meta.tmp", ".part.segments"} {
		other, err := m.AddTask(AddOptions{URL: "https://example.com/" + suffix, FileName: "file" + suffix})
		if err != nil {
			t.Fatal(err)
		}
		items, _ := m.Snapshot()
		it, _ := itemByID(items, other)
		if it.Path == path+suffix {
			t.Fatalf("overlapped %s", suffix)
		}
		if err := os.WriteFile(path+suffix, []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.Remove(id, false)
	paths := taskPaths(path)
	for _, p := range paths[1:] {
		if fileExists(p) {
			t.Fatalf("remove retained %s", p)
		}
	}
}

func TestRestoredSegmentPathsNeverOverlapOtherRecords(t *testing.T) {
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	path := m.dir + string(os.PathSeparator) + "first.bin"
	if err := os.WriteFile(path+".part.segments", []byte("owned by first"), 0600); err != nil {
		t.Fatal(err)
	}
	m.Restore([]Record{{ID: "first", URL: "https://example.com/first", Path: path}, {ID: "second", URL: "https://example.com/second", Path: path + ".part.segments", State: StateCompleted}})
	records := m.Records()
	if records[1].Path == path+".part.segments" || records[1].State != StatePaused || records[1].ConnectionLimit != 1 {
		t.Fatal("restore reused conflicting data or changed old default")
	}
	m.Remove("second", true)
	got, err := os.ReadFile(path + ".part.segments")
	if err != nil || string(got) != "owned by first" {
		t.Fatal("removal deleted another task's sparse file")
	}
}
