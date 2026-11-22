package download

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// VerificationState is independent of network transfer State. A mismatching
// digest never changes completed into failed or deletes the user's file.
type VerificationState string

const (
	VerificationUnconfigured VerificationState = ""
	VerificationPending      VerificationState = "pending"
	VerificationChecking     VerificationState = "checking"
	VerificationMatched      VerificationState = "matched"
	VerificationMismatched   VerificationState = "mismatched"
	VerificationCanceled     VerificationState = "canceled"
	VerificationFailed       VerificationState = "error"
)

// VerificationResult describes one historical attempt at a specific time. It
// is not evidence that a file remains unchanged after that attempt or restart.
type VerificationResult struct {
	State          VerificationState `json:"state"`
	ExpectedSHA256 string            `json:"expectedSHA256,omitempty"`
	ActualSHA256   string            `json:"actualSHA256,omitempty"`
	CheckedAt      time.Time         `json:"checkedAt"`
	Size           int64             `json:"size"`
	ModifiedAt     time.Time         `json:"modifiedAt"`
	Error          string            `json:"error,omitempty"`
}

func NormalizeSHA256(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) != 64 {
		return "", fmt.Errorf("SHA-256 必须为 64 位十六进制字符")
	}
	if _, err := hex.DecodeString(raw); err != nil {
		return "", fmt.Errorf("SHA-256 必须为 64 位十六进制字符")
	}
	return strings.ToLower(raw), nil
}

func resetVerificationLocked(t *task, expected string) {
	t.verifyGeneration++
	if t.verifyCancel != nil {
		t.verifyCancel()
	}
	t.verifyQueued = false
	t.item.ExpectedSHA256 = expected
	t.item.VerificationState = VerificationUnconfigured
	if expected != "" {
		t.item.VerificationState = VerificationPending
	}
	t.item.VerificationDone = 0
	t.item.VerificationTotal = 0
	t.item.VerificationError = ""
}

func cancelVerificationLocked(t *task) {
	t.verifyGeneration++
	t.verifyQueued = false
	if t.verifyCancel != nil {
		t.verifyCancel()
	}
	if t.item.VerificationState == VerificationPending || t.item.VerificationState == VerificationChecking {
		t.item.VerificationState = VerificationCanceled
	}
}

// Verify queues at most one job per existing task. One independent worker reads
// files at a time; a queue entry is a task flag, so repeated clicks cannot grow
// an unbounded work channel or occupy a network-concurrency slot.
func (m *Manager) Verify(id string) error {
	m.mu.Lock()
	t := m.byID[id]
	if m.closed || t == nil || t.removed {
		m.mu.Unlock()
		return fmt.Errorf("任务已移除或下载器正在退出")
	}
	if t.item.State != StateCompleted || t.item.ExpectedSHA256 == "" {
		m.mu.Unlock()
		return fmt.Errorf("请在传输完成并设置 SHA-256 后校验")
	}
	if t.verifyQueued || t.item.VerificationState == VerificationChecking {
		m.mu.Unlock()
		return nil
	}
	resetVerificationLocked(t, t.item.ExpectedSHA256)
	t.verifyQueued = true
	m.mu.Unlock()
	m.notify()
	m.kickVerification()
	return nil
}

func (m *Manager) CancelVerification(id string) {
	m.mu.Lock()
	if t := m.byID[id]; t != nil {
		cancelVerificationLocked(t)
	}
	m.mu.Unlock()
	m.notify()
}

func (m *Manager) kickVerification() {
	m.mu.Lock()
	if m.closed || m.verifyRunning {
		m.mu.Unlock()
		return
	}
	var next *task
	for _, t := range m.tasks {
		if t.verifyQueued && !t.removed && !t.working && t.item.State == StateCompleted {
			next = t
			break
		}
	}
	if next == nil {
		m.mu.Unlock()
		return
	}
	next.verifyQueued = false
	next.item.VerificationState = VerificationChecking
	ctx, cancel := context.WithCancel(context.Background())
	next.verifyCancel = cancel
	next.verifyDone = make(chan struct{})
	item, generation, read := next.item, next.verifyGeneration, m.verifyFile
	m.verifyRunning = true
	m.verifyWG.Add(1)
	m.mu.Unlock()
	m.notify()
	go func() {
		defer m.verifyWG.Done()
		result, identity := read(ctx, item.Path, item.ExpectedSHA256, func(done, total int64) {
			m.mu.Lock()
			if !next.removed && next.verifyGeneration == generation {
				next.item.VerificationDone = done
				next.item.VerificationTotal = total
			}
			m.mu.Unlock()
			m.notifyThrottled()
		})
		if result.State == VerificationMatched || result.State == VerificationMismatched {
			info, err := os.Stat(item.Path)
			if err != nil || !sameVerifiedFile(identity, info) {
				result.State = VerificationFailed
				result.ActualSHA256 = ""
				result.Error = "文件在校验期间发生变化或已缺失"
			}
		}
		m.mu.Lock()
		if !m.closed && !next.removed && next.verifyGeneration == generation && next.item.Path == item.Path && next.item.ExpectedSHA256 == item.ExpectedSHA256 && next.item.State == StateCompleted {
			if ctx.Err() != nil {
				result.State = VerificationCanceled
				result.ActualSHA256 = ""
				result.Error = ""
			}
			next.item.VerificationState = result.State
			next.item.VerificationError = result.Error
			next.item.LastVerification = result
		}
		cancel()
		next.verifyCancel = nil
		close(next.verifyDone)
		next.verifyDone = nil
		m.verifyRunning = false
		m.mu.Unlock()
		m.notify()
		m.kickVerification()
	}()
}

// SetExpectedSHA256 validates before changing the task. A new digest invalidates
// the current result; explicit verification (or transfer completion) runs it.
func (m *Manager) SetExpectedSHA256(id, raw string) error {
	expected, err := NormalizeSHA256(raw)
	if err != nil {
		return err
	}
	m.mu.Lock()
	t := m.byID[id]
	if m.closed || t == nil || t.removed {
		m.mu.Unlock()
		return fmt.Errorf("任务已移除或下载器正在退出")
	}
	if t.item.ExpectedSHA256 != expected {
		resetVerificationLocked(t, expected)
	}
	m.mu.Unlock()
	m.notify()
	return nil
}
