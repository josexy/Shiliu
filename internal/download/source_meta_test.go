package download

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sourceVersionServer(old, next []byte, same bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, etag := old, `"old"`
		if r.URL.Query().Get("v") == "new" {
			body = next
			if !same {
				etag = `"new"`
			}
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Disposition", `attachment; filename="server-renamed.bin"`)
		http.ServeContent(w, r, "body", time.Unix(1700000000, 0), bytes.NewReader(body))
	}))
}

func TestSourceCommitFailureAndInterruptedResetRecovery(t *testing.T) {
	old := bytes.Repeat([]byte("old"), 2048)
	next := bytes.Repeat([]byte("new"), 2048)
	server := sourceVersionServer(old, next, false)
	defer server.Close()
	for _, failure := range []string{"commit-before-write", "commit-after-write", "reset-before", "reset-after-truncate", "reset-meta-write"} {
		t.Run(failure, func(t *testing.T) {
			m := New(Options{Dir: t.TempDir()})
			id, path := sourcePartial(t, m, server.URL+"/file?v=old", old, 3000, `"old"`)
			oldRecords := m.Records()
			beforeMeta, _ := os.ReadFile(path + ".part.meta")
			d, err := m.BeginSourceUpdate(id, SourceOptions{URL: server.URL + "/file?v=new"})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := m.ProbeSource(context.Background(), d)
			if err != nil || preview.Reuse {
				t.Fatalf("probe: %+v %v", preview, err)
			}
			injected := errors.New("simulated interruption")
			if failure == "commit-before-write" {
				m.metaWrite = func(string, partMeta) error { return injected }
			}
			if failure == "commit-after-write" {
				m.metaWrite = func(p string, meta partMeta) error {
					if err := writePartMeta(p, meta); err != nil {
						return err
					}
					return injected
				}
			}
			err = m.CommitSource(d)
			part, _ := os.ReadFile(path + ".part")
			if !bytes.Equal(part, old[:3000]) {
				t.Fatal("commit truncated before explicit resume")
			}
			if failure == "commit-before-write" {
				meta, _ := os.ReadFile(path + ".part.meta")
				if err == nil || !bytes.Equal(meta, beforeMeta) || m.Records()[0].URL != oldRecords[0].URL {
					t.Fatal("failed commit altered original")
				}
				m.Shutdown()
				return
			}
			if failure == "commit-after-write" {
				if err == nil {
					t.Fatal("injected interruption absent")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			m.metaWrite = writePartMeta
			if failure == "reset-before" {
				m.resetPart = func(string) error { return injected }
			}
			if failure == "reset-after-truncate" {
				m.resetPart = func(p string) error {
					if err := resetPartFile(p); err != nil {
						return err
					}
					return injected
				}
			}
			if failure == "reset-meta-write" {
				m.metaWrite = func(string, partMeta) error { return injected }
			}
			if failure != "commit-after-write" {
				if err := m.applySourceReset(m.byID[id]); err == nil {
					t.Fatal("reset fault not injected")
				}
			}
			m.Shutdown()
			// Crash recovery sees records from before the source transaction.
			restored := New(Options{Dir: filepath.Dir(path)})
			defer restored.Shutdown()
			restored.Restore(oldRecords)
			items, _ := restored.Snapshot()
			if items[0].URL != server.URL+"/file?v=new" || !items[0].SourceReset || items[0].ID != id || items[0].Path != path {
				t.Fatalf("journal not recovered: %+v", items[0])
			}
			restored.Resume(id)
			it := waitState(t, restored, id, StateCompleted)
			got, err := os.ReadFile(it.Path)
			if err != nil || !bytes.Equal(got, next) {
				t.Fatal("recovery mixed source versions")
			}
		})
	}
}

func TestSourcePrefixReuseAndCompletionCheckpoint(t *testing.T) {
	data := bytes.Repeat([]byte("same"), 2048)
	server := sourceVersionServer(data, data, true)
	defer server.Close()
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	id, path := sourcePartial(t, m, server.URL+"/file?v=old", data, 3000, `"old"`)
	oldRecords := m.Records()
	d, err := m.BeginSourceUpdate(id, SourceOptions{URL: server.URL + "/file?v=new"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := m.ProbeSource(context.Background(), d)
	if err != nil || !preview.Reuse {
		t.Fatalf("probe: %+v %v", preview, err)
	}
	if err := m.CommitSource(d); err != nil {
		t.Fatal(err)
	}
	if m.Records()[0].Done != 3000 {
		t.Fatal("verified prefix was discarded")
	}
	// A successful paused-record save can retire the source-switch journal.
	pausedRecords := m.Records()
	if err := m.AcknowledgeSources(pausedRecords); err != nil {
		t.Fatal(err)
	}
	if readPartMeta(path+".part.meta").Source != nil {
		t.Fatal("saved source checkpoint retained")
	}
	m.Resume(id)
	it := waitState(t, m, id, StateCompleted)
	if it.Path != path || it.FileName != "fixed.bin" {
		t.Fatal("response renamed an updated source")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, data) {
		t.Fatal("resumed data mismatch")
	}
	meta := readPartMeta(path + ".part.meta")
	if meta.Source == nil || !meta.Source.Completed {
		t.Fatal("publication checkpoint missing")
	}
	if err := m.AcknowledgeSources(pausedRecords); err != nil {
		t.Fatal(err)
	}
	if !fileExists(path + ".part.meta") {
		t.Fatal("paused save erased unpublished completion")
	}
	restored := New(Options{Dir: filepath.Dir(path)})
	defer restored.Shutdown()
	restored.Restore(oldRecords)
	items, _ := restored.Snapshot()
	if items[0].State != StateCompleted || items[0].URL != server.URL+"/file?v=new" {
		t.Fatal("completion did not recover from stale records")
	}
	if err := m.AcknowledgeSources(m.Records()); err != nil {
		t.Fatal(err)
	}
	if fileExists(path+".part.meta") || fileExists(path+".part.meta.tmp") {
		t.Fatal("completed checkpoint not cleaned after durable save")
	}
}

func TestSourceCommitRechecksLocalPartialAndTemporaryReservation(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 100)
	server := sourceVersionServer(data, data, true)
	defer server.Close()
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	id, path := sourcePartial(t, m, server.URL+"/file?v=old", data, 50, `"old"`)
	d, err := m.BeginSourceUpdate(id, SourceOptions{URL: server.URL + "/file?v=new"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ProbeSource(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".part", []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitSource(d); err == nil {
		t.Fatal("changed partial was committed")
	}
	other, err := m.AddTask(AddOptions{URL: server.URL + "/other", FileName: "fixed.bin.part.meta.tmp"})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := m.Snapshot()
	it, _ := itemByID(items, other)
	if it.Path == path+".part.meta.tmp" {
		t.Fatal("atomic metadata temporary path was not reserved")
	}
}
