package tasklist

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDropBatchCombinedLimitsAndRegularFiles(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "one.txt")
	two := filepath.Join(dir, "two.TXT")
	os.WriteFile(one, []byte("\ufeffhttps://example.com/a\n"), 0600)
	os.WriteFile(two, []byte("https://example.com/b\nhttps://example.com/a"), 0600)
	p, err := ReadBatch(context.Background(), [][]byte{[]byte("https://example.com/text")}, []string{one, two}, nil)
	if err != nil || p.Valid != 3 || p.Duplicate != 1 {
		t.Fatal(p, err)
	}
	for _, path := range []string{dir, filepath.Join(dir, "bad.exe"), filepath.Join(dir, "shortcut.url")} {
		if path != dir {
			os.WriteFile(path, []byte("https://example.com/a"), 0600)
		}
		if _, err := ReadBatch(context.Background(), nil, []string{path}, nil); err == nil {
			t.Fatal("accepted non-TXT", path)
		}
	}
	os.WriteFile(two, []byte{0, 1, 2}, 0600)
	if _, err := ReadBatch(context.Background(), nil, []string{two}, nil); err == nil {
		t.Fatal("accepted binary TXT")
	}
	os.WriteFile(one, bytes.Repeat([]byte(" "), MaxBytes/2+1), 0600)
	os.WriteFile(two, bytes.Repeat([]byte(" "), MaxBytes/2+1), 0600)
	if _, err := ReadBatch(context.Background(), nil, []string{one, two}, nil); err == nil {
		t.Fatal("combined byte limit bypassed")
	}
	if _, err := ReadBatch(context.Background(), [][]byte{[]byte(strings.Repeat("https://example.com/a\n", MaxCandidates+1))}, nil, nil); err == nil {
		t.Fatal("combined candidate limit bypassed")
	}
	if _, err := ReadBatch(context.Background(), [][]byte{[]byte("https://example.com/" + strings.Repeat("a", MaxTokenBytes))}, nil, nil); err == nil {
		t.Fatal("token limit bypassed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadBatch(ctx, nil, []string{one}, nil); err != context.Canceled {
		t.Fatal(err)
	}
}
