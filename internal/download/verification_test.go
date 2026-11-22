package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSHA256InputAndHistoricalRestore(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
		bad       bool
	}{
		{"", "", false}, {" \t", "", false}, {" " + strings.Repeat("AB", 32) + "\n", strings.Repeat("ab", 32), false},
		{"abc", "", true}, {strings.Repeat("z", 64), "", true}, {strings.Repeat("a", 65), "", true},
	} {
		got, err := NormalizeSHA256(tc.raw)
		if (err != nil) != tc.bad || got != tc.want {
			t.Fatalf("normalization: %q %v", got, err)
		}
	}
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	digest := strings.Repeat("ab", 32)
	id, err := m.AddTask(AddOptions{URL: "example.com/check", ExpectedSHA256: strings.ToUpper(digest)})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetExpectedSHA256(id, "bad"); err == nil {
		t.Fatal("invalid digest accepted")
	}
	items, _ := m.Snapshot()
	if items[0].ExpectedSHA256 != digest || items[0].VerificationState != VerificationPending {
		t.Fatal("invalid edit replaced expected digest")
	}
	records := m.Records()
	records[0].State = StateCompleted
	records[0].LastVerification = VerificationResult{State: VerificationMatched, ExpectedSHA256: digest, ActualSHA256: digest, CheckedAt: time.Now(), Size: 10}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	var restored []Record
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	copy := New(Options{Dir: t.TempDir()})
	defer copy.Shutdown()
	copy.Restore(restored)
	items, _ = copy.Snapshot()
	if items[0].VerificationState != VerificationPending || items[0].LastVerification.State != VerificationMatched || items[0].ExpectedSHA256 != digest {
		t.Fatalf("historical match trusted as current: %+v", items[0])
	}
	if err := copy.SetExpectedSHA256(id, ""); err != nil {
		t.Fatal(err)
	}
	items, _ = copy.Snapshot()
	if items[0].VerificationState != VerificationUnconfigured {
		t.Fatal("clear did not disable checking")
	}
	restored[0].ID = "legacy"
	restored[0].ExpectedSHA256 = ""
	restored[0].LastVerification = VerificationResult{}
	copy.Restore(restored)
	items, _ = copy.Snapshot()
	if items[1].VerificationState != VerificationUnconfigured {
		t.Fatal("old record gained verification")
	}
}

func digestOf(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func waitVerification(t *testing.T, m *Manager, id string, want VerificationState) Item {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		items, _ := m.Snapshot()
		it, ok := itemByID(items, id)
		if ok && it.VerificationState == want {
			return it
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s: %+v", want, it)
		}
		time.Sleep(time.Millisecond)
	}
}

func completedVerificationTasks(t *testing.T, count int) (*Manager, []byte) {
	t.Helper()
	dir := t.TempDir()
	data := bytes.Repeat([]byte("verify-content"), 30000)
	m := New(Options{Dir: dir})
	var records []Record
	for i := range count {
		path := filepath.Join(dir, fmt.Sprintf("file%d.bin", i))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		records = append(records, Record{ID: fmt.Sprint(i), URL: "https://example.com/file", Path: path, State: StateCompleted, ExpectedSHA256: digestOf(data)})
	}
	m.Restore(records)
	return m, data
}

func TestFileHashMatchMismatchErrorsAndCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	data := bytes.Repeat([]byte("v"), 3*verificationChunkSize)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{digestOf(data), strings.Repeat("0", 64)} {
		last := int64(0)
		result, identity := verifyFileSHA256(context.Background(), path, expected, func(done, total int64) {
			if done-last > verificationChunkSize || total != int64(len(data)) {
				t.Error("unbounded verification chunk")
			}
			last = done
		})
		want := VerificationMismatched
		if expected == digestOf(data) {
			want = VerificationMatched
		}
		if result.State != want || result.ActualSHA256 != digestOf(data) || identity == nil || result.CheckedAt.IsZero() {
			t.Fatalf("hash result: %+v", result)
		}
	}
	for _, bad := range []string{filepath.Join(dir, "missing"), dir} {
		r, _ := verifyFileSHA256(context.Background(), bad, digestOf(data), func(int64, int64) {})
		if r.State != VerificationFailed || r.Error == "" {
			t.Fatal("unreadable file accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, _ := verifyFileSHA256(ctx, path, digestOf(data), func(done, _ int64) {
		if done > 0 {
			cancel()
		}
	})
	if result.State != VerificationCanceled || result.ActualSHA256 != "" {
		t.Fatal("canceled hash published a digest")
	}
}

func TestVerificationQueueIsSerialAndIndependent(t *testing.T) {
	m, _ := completedVerificationTasks(t, 2)
	defer m.Shutdown()
	entered := make(chan string, 2)
	gate := make(chan struct{})
	var active, maximum atomic.Int32
	m.verifyFile = func(ctx context.Context, path, expected string, progress func(int64, int64)) (VerificationResult, os.FileInfo) {
		n := active.Add(1)
		if n > maximum.Load() {
			maximum.Store(n)
		}
		defer active.Add(-1)
		entered <- path
		select {
		case <-gate:
		case <-ctx.Done():
		}
		return verifyFileSHA256(ctx, path, expected, progress)
	}
	if err := m.Verify("0"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	if err := m.Verify("1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify("0"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	networkSlots := m.active
	m.mu.Unlock()
	if networkSlots != 0 || len(entered) != 0 {
		t.Fatal("verification used a network slot or ran concurrently")
	}
	gate <- struct{}{}
	waitVerification(t, m, "0", VerificationMatched)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("second job not started")
	}
	gate <- struct{}{}
	waitVerification(t, m, "1", VerificationMatched)
	if maximum.Load() != 1 {
		t.Fatal("more than one hash worker")
	}
	items, _ := m.Snapshot()
	for _, it := range items {
		if it.State != StateCompleted {
			t.Fatal("hash changed transfer state")
		}
	}
}

func TestVerificationCancelAndReadFailureState(t *testing.T) {
	m, _ := completedVerificationTasks(t, 1)
	defer m.Shutdown()
	entered := make(chan struct{})
	m.verifyFile = func(ctx context.Context, _ string, _ string, _ func(int64, int64)) (VerificationResult, os.FileInfo) {
		close(entered)
		<-ctx.Done()
		return VerificationResult{State: VerificationCanceled}, nil
	}
	if err := m.Verify("0"); err != nil {
		t.Fatal(err)
	}
	<-entered
	m.CancelVerification("0")
	waitVerification(t, m, "0", VerificationCanceled)
	m.verifyWG.Wait()
	m.verifyFile = func(context.Context, string, string, func(int64, int64)) (VerificationResult, os.FileInfo) {
		return VerificationResult{State: VerificationFailed, Error: "读取失败"}, nil
	}
	if err := m.Verify("0"); err != nil {
		t.Fatal(err)
	}
	it := waitVerification(t, m, "0", VerificationFailed)
	if it.State != StateCompleted || it.VerificationError != "读取失败" {
		t.Fatal("read error affected transfer or disappeared")
	}
}

func TestHashRejectsFileChangesDuringReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changing.bin")
	data := bytes.Repeat([]byte("x"), 3*verificationChunkSize)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	changed := false
	result, _ := verifyFileSHA256(context.Background(), path, digestOf(data), func(done, _ int64) {
		if done > 0 && !changed {
			changed = true
			if err := os.Truncate(path, 10); err != nil {
				t.Error(err)
			}
		}
	})
	if result.State != VerificationFailed || result.ActualSHA256 != "" {
		t.Fatal("changed file published a digest")
	}
}

func TestVerificationRejectsReplacedOrDeletedPath(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint(remove), func(t *testing.T) {
			m, data := completedVerificationTasks(t, 1)
			defer m.Shutdown()
			m.verifyFile = func(ctx context.Context, path, expected string, progress func(int64, int64)) (VerificationResult, os.FileInfo) {
				r, info := verifyFileSHA256(ctx, path, expected, progress)
				if err := os.Rename(path, path+".old"); err != nil {
					t.Error(err)
				}
				if !remove {
					if err := os.WriteFile(path, bytes.Repeat([]byte("z"), len(data)), 0600); err != nil {
						t.Error(err)
					}
					if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
						t.Error(err)
					}
				}
				return r, info
			}
			if err := m.Verify("0"); err != nil {
				t.Fatal(err)
			}
			it := waitVerification(t, m, "0", VerificationFailed)
			if it.LastVerification.ActualSHA256 != "" {
				t.Fatal("replacement retained trusted digest")
			}
		})
	}
}

func TestDigestChangeRejectsLateResult(t *testing.T) {
	m, _ := completedVerificationTasks(t, 1)
	entered := make(chan struct{})
	release := make(chan struct{})
	defer m.Shutdown()
	m.verifyFile = func(ctx context.Context, path, expected string, progress func(int64, int64)) (VerificationResult, os.FileInfo) {
		r, info := verifyFileSHA256(ctx, path, expected, progress)
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return r, info
	}
	if err := m.Verify("0"); err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := m.SetExpectedSHA256("0", strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	close(release)
	m.verifyWG.Wait()
	items, _ := m.Snapshot()
	if items[0].VerificationState != VerificationPending || items[0].LastVerification.State == VerificationMatched {
		t.Fatal("late result became current after digest edit")
	}
}

func TestRemovalAndShutdownWaitForVerification(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint(remove), func(t *testing.T) {
			m, _ := completedVerificationTasks(t, 1)
			defer m.Shutdown()
			entered := make(chan struct{})
			released := make(chan struct{})
			m.verifyFile = func(ctx context.Context, path, expected string, progress func(int64, int64)) (VerificationResult, os.FileInfo) {
				f, err := os.Open(path)
				if err != nil {
					t.Error(err)
				}
				close(entered)
				<-ctx.Done()
				if f != nil {
					f.Close()
				}
				close(released)
				return VerificationResult{State: VerificationCanceled}, nil
			}
			if err := m.Verify("0"); err != nil {
				t.Fatal(err)
			}
			<-entered
			items, _ := m.Snapshot()
			path := items[0].Path
			if remove {
				m.Remove("0", true)
			} else {
				m.Shutdown()
			}
			select {
			case <-released:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("file handle remained open")
			}
			m.verifyWG.Wait()
			if remove {
				deadline := time.Now().Add(time.Second)
				for {
					_, err := os.Stat(path)
					if os.IsNotExist(err) {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("file was not removed after hash released it")
					}
					time.Sleep(time.Millisecond)
				}
				items, _ = m.Snapshot()
				if len(items) != 0 {
					t.Fatal("removed task resurrected")
				}
			}
		})
	}
}

func TestRefreshInvalidatesPreviouslyVerifiedFile(t *testing.T) {
	m, _ := completedVerificationTasks(t, 1)
	defer m.Shutdown()
	if err := m.Verify("0"); err != nil {
		t.Fatal(err)
	}
	it := waitVerification(t, m, "0", VerificationMatched)
	if err := os.WriteFile(it.Path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	m.RefreshFiles()
	it = waitVerification(t, m, "0", VerificationFailed)
	if it.LastVerification.State != VerificationMatched {
		t.Fatal("historical result was erased")
	}
}

func TestTransferCompletionQueuesSeparateVerification(t *testing.T) {
	data := []byte("download succeeded even when the expected digest is wrong")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	defer srv.Close()
	var completions atomic.Int32
	m := New(Options{Dir: t.TempDir(), AutoStart: true, OnComplete: func(it Item) {
		completions.Add(1)
		if it.State != StateCompleted || (it.ExpectedSHA256 != "" && it.VerificationState != VerificationPending) {
			t.Error("completion claimed verification")
		}
	}})
	defer m.Shutdown()
	id, err := m.AddTask(AddOptions{URL: srv.URL + "/data.bin", ExpectedSHA256: strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	it := waitVerification(t, m, id, VerificationMismatched)
	got, err := os.ReadFile(it.Path)
	if err != nil || !bytes.Equal(got, data) || it.State != StateCompleted || completions.Load() != 1 {
		t.Fatal("mismatch deleted/retried/failed the transfer")
	}
	if it.LastVerification.ActualSHA256 != digestOf(data) {
		t.Fatal("actual digest missing")
	}
	id, err = m.AddTask(AddOptions{URL: srv.URL + "/unchecked.bin"})
	if err != nil {
		t.Fatal(err)
	}
	it = waitState(t, m, id, StateCompleted)
	if it.VerificationState != VerificationUnconfigured || it.LastVerification.ActualSHA256 != "" {
		t.Fatal("unconfigured task was hashed")
	}
}
