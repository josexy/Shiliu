package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExportMetadataEmptyAndAtomicFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "diagnostics.json")
	if err := Export(context.Background(), path, "1.2.3", Snapshot{}); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	var d Document
	if err := json.Unmarshal(original, &d); err != nil || d.Version != 1 || d.AppVersion != "1.2.3" || d.Platform != runtime.GOOS+"/"+runtime.GOARCH || d.Events == nil {
		t.Fatal(d, err)
	}
	for _, cancelWrite := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		err := exportAtomic(ctx, path, func(f *os.File) error {
			f.Write([]byte("partial"))
			if cancelWrite {
				cancel()
				return nil
			}
			return io.ErrShortWrite
		})
		cancel()
		if err == nil {
			t.Fatal("failed export succeeded")
		}
		got, _ := os.ReadFile(path)
		if string(got) != string(original) {
			t.Fatal("old export overwritten")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Export(ctx, path, "dev", Snapshot{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := Export(context.Background(), filepath.Join(dir, "missing", "file"), "dev", Snapshot{}); err == nil {
		t.Fatal("missing directory accepted")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".diagnostics-") {
			t.Fatal("temporary file leaked")
		}
	}
}
