// Package tasklist reads bounded TXT link lists and exports sanitized sharing
// lists. It does not implement task backups or restore credentials from disk.
package tasklist

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"go-downloader/internal/download"
)

const (
	MaxBytes      = 10 * 1024 * 1024
	MaxTokenBytes = 16 * 1024
	MaxCandidates = 10000
)

type Kind string

const (
	Valid     Kind = "有效"
	Duplicate Kind = "重复"
	Invalid   Kind = "无效"
)

// Entry contains only display-safe data; Index is the one-based token position.
type Entry struct {
	Index int
	URL   string
	Kind  Kind
}

// Preview retains original links privately in memory for submission to Add.
// Copying displayed URLs back into Add would silently lose authentication.
type Preview struct {
	Entries                   []Entry
	Valid, Duplicate, Invalid int
	candidates                []string
}

// Candidates returns valid raw links, including duplicates for engine-side
// rechecking. Callers must not persist or display these values.
func (p Preview) Candidates() []string { return append([]string(nil), p.candidates...) }

// Parse accepts UTF-8 with an optional leading BOM and the same separators as
// pasted links. Encoding or capacity errors reject the entire file. Invalid
// tokens are represented by position only, since even malformed input can hold
// secrets. Existing-task duplicate counts are advisory until confirmation.
func Parse(ctx context.Context, data []byte, existing []download.Item) (Preview, error) {
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	if len(data) > MaxBytes {
		return Preview{}, fmt.Errorf("TXT 文件不能超过 10 MiB")
	}
	if !utf8.Valid(data) {
		return Preview{}, fmt.Errorf("TXT 文件必须使用 UTF-8 编码")
	}
	tokens := download.URLTokens(strings.TrimPrefix(string(data), "\ufeff"))
	if len(tokens) > MaxCandidates {
		return Preview{}, fmt.Errorf("一次最多导入 10000 条候选链接")
	}
	seen := make(map[string]bool, len(existing)+len(tokens))
	for _, it := range existing {
		if !it.State.Terminal() {
			seen[it.URL] = true
		}
	}
	p := Preview{}
	for i, raw := range tokens {
		if err := ctx.Err(); err != nil {
			return Preview{}, err
		}
		if len(raw) > MaxTokenBytes {
			return Preview{}, fmt.Errorf("第 %d 条候选超过 16 KiB", i+1)
		}
		valid, invalid := download.NormalizeURLs(raw)
		entry := Entry{Index: i + 1, Kind: Invalid, URL: "（无效链接）"}
		if len(valid) == 1 && len(invalid) == 0 {
			entry.URL = download.DiagnosticURL(valid[0])
			entry.Kind = Valid
			if seen[valid[0]] {
				entry.Kind = Duplicate
				p.Duplicate++
			} else {
				p.Valid++
			}
			seen[valid[0]] = true
			p.candidates = append(p.candidates, raw)
		} else {
			p.Invalid++
		}
		p.Entries = append(p.Entries, entry)
	}
	return p, nil
}
