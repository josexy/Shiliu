package download

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// sourceJournal bridges the atomic sidecar commit and the application's next
// successful records save. Reset is cleared only after truncation and Sync;
// Completed is written before final rename and retired after a completed record
// has been saved. Neither checkpoint contains credentials or custom headers.
type sourceJournal struct {
	TaskID          string    `json:"taskId"`
	Revision        uint64    `json:"revision"`
	URL             string    `json:"url"`
	Reset           bool      `json:"reset,omitempty"`
	HeadersRequired bool      `json:"headersRequired,omitempty"`
	Completed       bool      `json:"completed,omitempty"`
	FinishedAt      time.Time `json:"finishedAt,omitzero"`
}

func validSourceJournal(s *sourceJournal, id string) bool {
	if s == nil || s.TaskID != id || s.Revision == 0 {
		return false
	}
	t, err := parseTarget(s.URL)
	return err == nil && t.user == "" && t.pass == "" && t.display == s.URL
}

func sourcePartUnchanged(d *SourceDraft) bool {
	path := d.partPath
	if path == "" {
		path = d.old.Path + ".part"
	}
	info, err := os.Stat(path)
	if d.partInfo == nil {
		return errors.Is(err, os.ErrNotExist)
	}
	return err == nil && sameVerifiedFile(d.partInfo, info)
}

func (m *Manager) sourceJobDone(t *task, tracked bool) {
	m.mu.Lock()
	t.sourceWorking--
	m.mu.Unlock()
	t.sourceJobs.Done()
	if tracked {
		m.sourceWG.Done()
	}
}

func sourceJournalLocked(t *task) *sourceJournal {
	return &sourceJournal{TaskID: t.item.ID, Revision: t.item.SourceRevision, URL: t.item.URL, Reset: t.item.SourceReset, HeadersRequired: len(t.headers) > 0 || t.item.NeedsHeaders}
}

func resetPartFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// applySourceReset runs before opening a network response. A crash before the
// second atomic write leaves Reset set, so repeating this operation is safe.
func (m *Manager) applySourceReset(t *task) error {
	t.metaMu.Lock()
	defer t.metaMu.Unlock()
	m.mu.Lock()
	path, reset := t.item.Path, t.item.SourceReset
	m.mu.Unlock()
	meta := readPartMeta(path + ".part.meta")
	reset = reset || (validSourceJournal(meta.Source, t.item.ID) && meta.Source.Reset)
	if !reset {
		return nil
	}
	if err := m.resetPart(path + ".part"); err != nil {
		return fmt.Errorf("无法重置部分文件：%w", err)
	}
	if err := os.Remove(path + ".part.segments"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("无法重置分片文件：%w", err)
	}
	m.mu.Lock()
	t.item.SourceReset = false
	t.item.Done = 0
	t.etag = ""
	t.lastModified = ""
	source := sourceJournalLocked(t)
	m.mu.Unlock()
	meta = partMeta{URL: source.URL, Total: -1, Source: source}
	if err := m.metaWrite(path+".part.meta", meta); err != nil {
		return fmt.Errorf("无法保存重置结果：%w", err)
	}
	return nil
}

func (m *Manager) writeTaskMeta(t *task, path string, meta partMeta) error {
	t.metaMu.Lock()
	defer t.metaMu.Unlock()
	m.mu.Lock()
	if t.item.SourceRevision > t.sourceAcknowledged || t.item.SourceReset {
		meta.Source = sourceJournalLocked(t)
	}
	m.mu.Unlock()
	return m.metaWrite(path, meta)
}

func (m *Manager) prepareSourceCompletion(t *task, path string, size int64) (bool, error) {
	t.metaMu.Lock()
	defer t.metaMu.Unlock()
	m.mu.Lock()
	if t.item.SourceRevision == 0 {
		m.mu.Unlock()
		return false, nil
	}
	source := sourceJournalLocked(t)
	source.Reset = false
	source.Completed = true
	source.FinishedAt = timeNow()
	m.mu.Unlock()
	meta := readPartMeta(path)
	meta.URL = source.URL
	meta.Total = size
	meta.Source = source
	return true, m.metaWrite(path, meta)
}

// discardPart preserves a source checkpoint when an unsafe range response
// forces a fresh transfer; deleting all metadata here could restore an old URL.
func (m *Manager) discardPart(t *task, partPath, metaPath string) error {
	t.metaMu.Lock()
	defer t.metaMu.Unlock()
	if err := os.Remove(partPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	t.etag = ""
	t.lastModified = ""
	t.item.Done = 0
	meta := partMeta{URL: t.item.URL, Total: -1}
	if t.item.SourceRevision > 0 {
		meta.Source = sourceJournalLocked(t)
		meta.Source.Reset = false
	}
	m.mu.Unlock()
	if meta.Source != nil {
		return m.metaWrite(metaPath, meta)
	}
	if err := os.Remove(metaPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// AcknowledgeSources may retire journals only after these exact records were
// saved successfully. The caller must serialize snapshot+save+ack, preventing
// an older snapshot from overwriting a newly acknowledged source revision.
func (m *Manager) AcknowledgeSources(records []Record) error {
	for _, record := range records {
		if record.SourceRevision == 0 || record.SourceReset {
			continue
		}
		m.mu.Lock()
		t := m.byID[record.ID]
		if t == nil || t.removed {
			m.mu.Unlock()
			continue
		}
		t.sourceWorking++
		t.sourceJobs.Add(1)
		m.mu.Unlock()
		err := m.acknowledgeSource(t, record)
		m.sourceJobDone(t, false)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) acknowledgeSource(t *task, record Record) error {
	t.metaMu.Lock()
	defer t.metaMu.Unlock()
	m.mu.Lock()
	if t.removed || t.item.SourceRevision != record.SourceRevision || t.item.URL != record.URL || t.item.SourceReset {
		m.mu.Unlock()
		return nil
	}
	t.sourceAcknowledged = record.SourceRevision
	path := t.item.Path + ".part.meta"
	completed := t.item.State == StateCompleted && record.State == StateCompleted && t.item.FinishedAt.Equal(record.FinishedAt)
	m.mu.Unlock()
	meta := readPartMeta(path)
	if !validSourceJournal(meta.Source, record.ID) || meta.Source.Revision != record.SourceRevision {
		return nil
	}
	if meta.Source.Completed {
		if !completed {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if meta.Source.Reset {
		return nil
	}
	meta.Source = nil
	return m.metaWrite(path, meta)
}
