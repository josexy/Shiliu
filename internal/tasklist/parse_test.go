package tasklist

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"go-downloader/internal/download"
)

func TestParsePreviewAndPrivateCandidates(t *testing.T) {
	raw := "\ufeffhttps://user:secret@example.com/a?token=private#fragment，example.com/b\tftp://user:secret@host/a\nnot-a-link\u2003https://example.com/a?token=private"
	existing := []download.Item{{URL: "https://example.com/b", State: download.StatePaused}}
	p, err := Parse(context.Background(), []byte(raw), existing)
	if err != nil || p.Valid != 1 || p.Duplicate != 2 || p.Invalid != 2 {
		t.Fatalf("counts: %+v, %v", p, err)
	}
	if p.Entries[2].Index != 3 || p.Entries[4].Kind != Duplicate {
		t.Fatalf("positions: %+v", p.Entries)
	}
	if display := fmt.Sprint(p.Entries); strings.Contains(display, "secret") || strings.Contains(display, "private") || strings.Contains(display, "user") {
		t.Fatal("preview leaked credentials")
	}
	candidates := p.Candidates()
	if len(candidates) != 3 || !strings.Contains(candidates[0], "user:secret@") {
		t.Fatal("submission lost authentication")
	}
	candidates[0] = "changed"
	if p.Candidates()[0] == "changed" {
		t.Fatal("candidate copy aliases preview")
	}
}

func TestParseCapacityAndEncoding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		reject bool
	}{
		{"bytes-boundary", []byte(strings.Repeat(" ", MaxBytes)), false},
		{"bytes-over", []byte(strings.Repeat(" ", MaxBytes+1)), true},
		{"token-boundary", []byte("https://example.com/" + strings.Repeat("x", MaxTokenBytes-len("https://example.com/"))), false},
		{"token-over", []byte("https://example.com/" + strings.Repeat("x", MaxTokenBytes)), true},
		{"count-boundary", []byte(strings.Repeat("example.com\n", MaxCandidates)), false},
		{"count-over", []byte(strings.Repeat("example.com\n", MaxCandidates+1)), true},
		{"invalid-utf8", []byte{0xff}, true},
		{"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Parse(context.Background(), tc.data, nil)
			if (err != nil) != tc.reject {
				t.Fatalf("error = %v", err)
			}
			if tc.reject && (len(p.Entries) != 0 || len(p.Candidates()) != 0) {
				t.Fatal("rejected file has partial results")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Parse(ctx, []byte("example.com"), nil); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
}
