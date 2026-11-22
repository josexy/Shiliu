package download

import (
	"net/http"
	"strings"
	"testing"
)

// Isolate admission/commit from HTTP probing in these state-machine tests.
func readySourceDraft(t *testing.T, m *Manager, id, url string) *SourceDraft {
	t.Helper()
	d, err := m.BeginSourceUpdate(id, SourceOptions{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	d.validated = true
	d.preview = SourcePreview{URL: DiagnosticURL(url), Total: -1}
	return d
}

func TestSourceUpdateAdmissionAndPreservedConfiguration(t *testing.T) {
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	id, err := m.AddTask(AddOptions{URL: "https://old:password@example.com/old", FileName: "chosen.bin", TaskRateLimit: 4096, ExpectedSHA256: strings.Repeat("a", 64), Headers: http.Header{"Cookie": {"old=secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	before := m.Records()[0]
	d := readySourceDraft(t, m, id, "https://example.com/new")
	if err := m.CommitSource(d); err != nil {
		t.Fatal(err)
	}
	after := m.Records()[0]
	if after.ID != before.ID || after.Path != before.Path || after.FileName != before.FileName || !after.CreatedAt.Equal(before.CreatedAt) || after.TaskRateLimit != before.TaskRateLimit || after.ExpectedSHA256 != before.ExpectedSHA256 || after.State != StatePaused {
		t.Fatalf("update lost identity/configuration: %+v", after)
	}
	if err := m.CommitSource(d); err == nil {
		t.Fatal("draft committed twice")
	}
	m.mu.Lock()
	task := m.byID[id]
	if task.user != "" || task.pass != "" || len(task.headers) != 0 {
		t.Error("old credentials migrated")
	}
	m.mu.Unlock()
	for _, state := range []State{StateQueued, StateConnecting, StateDownloading, StateCompleted} {
		m.mu.Lock()
		task.item.State = state
		m.mu.Unlock()
		if _, err := m.BeginSourceUpdate(id, SourceOptions{URL: "example.com/next"}); err == nil {
			t.Fatalf("accepted state %s", state)
		}
	}
	m.mu.Lock()
	task.item.State = StatePaused
	task.working = true
	m.mu.Unlock()
	if _, err := m.BeginSourceUpdate(id, SourceOptions{URL: "example.com/next"}); err == nil {
		t.Fatal("accepted still-stopping worker")
	}
	m.mu.Lock()
	task.working = false
	m.mu.Unlock()
}

func TestSourceUpdateDuplicateAndResumeConflicts(t *testing.T) {
	m := New(Options{Dir: t.TempDir(), ClientFactory: func() (*http.Client, error) { return nil, errRemoved }})
	defer m.Shutdown()
	id := addOne(t, m, "https://example.com/old")
	d := readySourceDraft(t, m, id, "https://example.com/new")
	other := addOne(t, m, "https://example.com/new")
	if err := m.CommitSource(d); err == nil {
		t.Fatal("commit merged duplicate task")
	}
	m.Remove(other, false)
	d = readySourceDraft(t, m, id, "https://example.com/new")
	m.Resume(id)
	if err := m.CommitSource(d); err == nil {
		t.Fatal("resume did not invalidate draft")
	}
	items, _ := m.Snapshot()
	if items[0].URL != "https://example.com/old" {
		t.Fatal("conflict changed original URL")
	}
}
