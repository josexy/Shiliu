package download

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

const segmentSize int64 = 4 * 1024 * 1024
const segmentThreshold int64 = 2 * segmentSize

type byteRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

func (r byteRange) size() int64 { return r.End - r.Start + 1 }
func segmentAt(start, total int64) byteRange {
	return byteRange{start, start + min(segmentSize, total-start) - 1}
}
func completedBytes(meta partMeta) int64 {
	var n int64
	for _, r := range meta.Completed {
		n += r.size()
	}
	return n
}

func validSegments(meta partMeta, url string) bool {
	if meta.SchemaVersion != 2 || meta.Mode != "segments" || meta.URL != url || meta.Total < segmentThreshold || !strongETag(meta.ETag) {
		return false
	}
	t, err := parseTarget(meta.ResourceURL)
	if err != nil || t.display != meta.ResourceURL || t.user != "" || t.pass != "" {
		return false
	}
	var end int64 = -1
	for _, r := range meta.Completed {
		if r.Start < 0 || r.Start >= meta.Total || r.Start%segmentSize != 0 || r.Start <= end || r.End != segmentAt(r.Start, meta.Total).End || r.End >= meta.Total || r.End < r.Start {
			return false
		}
		end = r.End
	}
	return true
}

func segmentFileValid(path string, meta partMeta) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() == meta.Total
}

func segmentsCover(meta partMeta) bool {
	var next int64
	for _, r := range meta.Completed {
		if r.Start != next {
			return false
		}
		next = r.End + 1
	}
	return next == meta.Total
}

// Only a synced whole range becomes durable progress. The lock serializes
// sidecar replacements, while disjoint WriteAt operations remain concurrent.
func (m *Manager) commitSegment(t *task, f *os.File, meta *partMeta, r byteRange) error {
	t.metaMu.Lock()
	defer t.metaMu.Unlock()
	if err := m.syncSegments(f); err != nil {
		return fmt.Errorf("分片同步失败：%w", err)
	}
	next := *meta
	next.Completed = append(append([]byteRange(nil), meta.Completed...), r)
	sort.Slice(next.Completed, func(i, j int) bool { return next.Completed[i].Start < next.Completed[j].Start })
	m.mu.Lock()
	path := t.item.Path
	if t.item.SourceRevision > t.sourceAcknowledged {
		next.Source = sourceJournalLocked(t)
	} else {
		next.Source = nil
	}
	m.mu.Unlock()
	if err := m.metaWrite(path+".part.meta", next); err != nil {
		return fmt.Errorf("分片记录保存失败：%w", err)
	}
	*meta = next
	m.mu.Lock()
	t.segmentCommitted = completedBytes(next)
	m.mu.Unlock()
	return nil
}

func (m *Manager) discardSegments(t *task, reason string) error {
	t.metaMu.Lock()
	defer t.metaMu.Unlock()
	m.mu.Lock()
	path := t.item.Path
	meta := partMeta{URL: t.item.URL, Total: -1}
	if t.item.SourceRevision > 0 {
		meta.Source = sourceJournalLocked(t)
		meta.Source.Reset = false
	}
	m.mu.Unlock()
	if err := os.Remove(path + ".part.segments"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := m.metaWrite(path+".part.meta", meta); err != nil {
		return err
	}
	m.mu.Lock()
	t.item.Done = 0
	t.item.TransferMode = "single"
	t.item.FallbackReason = reason
	t.segmentCommitted = 0
	t.etag = ""
	t.lastModified = ""
	m.mu.Unlock()
	return nil
}
