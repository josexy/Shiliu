package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"github.com/egoist/mygo/transfer"
	"go-downloader/internal/download"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxClipboardBytes = 1024 * 1024

type clipboardState struct {
	off                  func()
	generation, sequence uint64
	cancel               context.CancelFunc
	pending              []string
	cache                clipboardCache
}
type clipboardCache struct {
	salt   [32]byte
	ready  bool
	recent [][32]byte
}

func (c *clipboardCache) fingerprint(kind, content string) [32]byte {
	if !c.ready {
		rand.Read(c.salt[:])
		c.ready = true
	}
	h := sha256.New()
	h.Write(c.salt[:])
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(content))
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}
func (c *clipboardCache) seen(key [32]byte) bool {
	for i, k := range c.recent {
		if k == key {
			copy(c.recent[i:], c.recent[i+1:])
			c.recent[len(c.recent)-1] = key
			return true
		}
	}
	return false
}
func (c *clipboardCache) remember(key [32]byte) {
	if c.seen(key) {
		return
	}
	if len(c.recent) == 256 {
		copy(c.recent, c.recent[1:])
		c.recent = c.recent[:255]
	}
	c.recent = append(c.recent, key)
}

// MyGo materializes native data before returning it and has no pre-read byte
// limit. This cap bounds app retention/parsing, not transient OS allocations.
// No URI, HTML, image, or other representation is requested.
func clipboardText(formats func() []transfer.Format, read func() (transfer.Data, error)) ([]byte, error) {
	if !slices.Contains(formats(), transfer.Text) {
		return nil, nil
	}
	data, err := read()
	if err != nil {
		return nil, err
	}
	text, err := data.Read(transfer.Text)
	if err != nil {
		return nil, err
	}
	if len(text) > maxClipboardBytes {
		return nil, nil
	}
	return text, nil
}

func parseClipboardLinks(ctx context.Context, text []byte, existing []download.Item) (raw []string, canonical string) {
	if len(text) > maxClipboardBytes || !utf8.Valid(text) {
		return nil, ""
	}
	seen := map[string]bool{}
	for _, it := range existing {
		seen[it.URL] = true
	}
	var urls []string
	for _, token := range download.URLTokens(string(text)) {
		if ctx.Err() != nil {
			return nil, ""
		}
		lower := strings.ToLower(token)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			continue
		}
		valid, invalid := download.NormalizeURLs(token)
		if len(valid) != 1 || len(invalid) != 0 || seen[valid[0]] {
			continue
		}
		seen[valid[0]] = true
		raw = append(raw, token)
		urls = append(urls, valid[0])
	}
	sort.Strings(urls)
	return raw, strings.Join(urls, "\n")
}

func (a *app) setClipboardPrompts(on bool) {
	a.s.ClipboardPrompts = on
	a.syncClipboard()
	a.saveSettings()
}
func (a *app) syncClipboard() {
	if a.stopping || !a.s.ClipboardPrompts {
		a.stopClipboard()
		return
	}
	if a.clipboard.off != nil || a.deps.watchClipboard == nil || a.deps.readClipboardText == nil {
		return
	}
	a.clipboard.generation++
	generation := a.clipboard.generation
	a.clipboard.off = a.deps.watchClipboard(func() {
		// Test before any read: a callback can already be queued when off runs.
		if a.stopping || !a.s.ClipboardPrompts || generation != a.clipboard.generation {
			return
		}
		a.clipboardChanged(generation)
	})
	if a.clipboard.off == nil {
		a.clipboard.off = func() {}
	}
}
func (a *app) stopClipboard() {
	a.clipboard.generation++
	a.clipboard.sequence++
	if a.clipboard.cancel != nil {
		a.clipboard.cancel()
		a.clipboard.cancel = nil
	}
	off := a.clipboard.off
	a.clipboard.off = nil
	a.clipboard.pending = nil
	if off != nil {
		off()
	}
}
func (a *app) clipboardChanged(generation uint64) {
	a.clipboard.sequence++
	seq := a.clipboard.sequence
	if a.clipboard.cancel != nil {
		a.clipboard.cancel()
	}
	a.clipboard.pending = nil
	text, err := a.deps.readClipboardText()
	if err != nil || len(text) == 0 || len(text) > maxClipboardBytes {
		return
	}
	if a.clipboard.cache.seen(a.clipboard.cache.fingerprint("copy", string(text))) {
		return
	}
	ctx, cancel := context.WithCancel(a.lifetime)
	a.clipboard.cancel = cancel
	items, _ := a.engine.Snapshot()
	a.backgroundJobs.Add(1)
	a.deps.async(func() {
		defer a.backgroundJobs.Done()
		defer cancel()
		raw, canonical := parseClipboardLinks(ctx, text, items)
		canceled := ctx.Err() != nil
		a.dispatch(func() {
			if a.stopping || !a.s.ClipboardPrompts || a.clipboard.generation != generation || a.clipboard.sequence != seq || canceled {
				return
			}
			if len(raw) == 0 {
				return
			}
			key := a.clipboard.cache.fingerprint("links", canonical)
			if a.clipboard.cache.seen(key) {
				return
			}
			a.clipboard.cache.remember(key)
			a.clipboard.pending = raw
		})
	})
}
func (a *app) suppressClipboardCopy(text string) {
	if !a.s.ClipboardPrompts {
		return
	}
	a.clipboard.cache.remember(a.clipboard.cache.fingerprint("copy", text))
	_, canonical := parseClipboardLinks(context.Background(), []byte(text), nil)
	if canonical != "" {
		a.clipboard.cache.remember(a.clipboard.cache.fingerprint("links", canonical))
	}
}
func (a *app) ignoreClipboard() { a.clipboard.pending = nil }
func (a *app) addClipboardLinks() {
	if len(a.clipboard.pending) == 0 || a.stopping {
		return
	}
	raw := a.clipboard.pending
	a.clipboard.pending = nil
	ctx, engine := a.lifetime, a.engine
	a.backgroundJobs.Add(1)
	a.deps.async(func() {
		defer a.backgroundJobs.Done()
		result := submitImportCandidates(ctx, engine, raw)
		a.dispatch(func() {
			if !a.stopping {
				a.dirty.Store(true)
				a.toast(fmt.Sprintf("剪贴板链接：已添加 %d · 重复 %d · 失败 %d", result.added, result.skipped, result.failed))
			}
		})
	})
}
