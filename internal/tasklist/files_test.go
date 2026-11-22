package tasklist

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestReadFailureAndCancel(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(context.Background(), filepath.Join(dir, "missing.txt"), nil); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err := Read(context.Background(), dir, nil); err == nil {
		t.Fatal("directory accepted")
	}
	path := filepath.Join(dir, "links.txt")
	if err := os.WriteFile(path, []byte("\ufeffexample.com/a"), 0600); err != nil {
		t.Fatal(err)
	}
	if p, err := Read(context.Background(), path, nil); err != nil || p.Valid != 1 {
		t.Fatalf("read: %+v %v", p, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := readerFunc(func(p []byte) (int, error) { cancel(); return copy(p, "example.com/a"), nil })
	if _, err := readBounded(ctx, r); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	failure := errors.New("read failure")
	if _, err := readBounded(context.Background(), readerFunc(func([]byte) (int, error) { return 0, failure })); !errors.Is(err, failure) {
		t.Fatalf("read failure: %v", err)
	}
	if _, err := readBounded(context.Background(), strings.NewReader(strings.Repeat("x", MaxBytes+1))); err == nil {
		t.Fatal("unbounded read")
	}
}

func TestAtomicExportKeepsOriginalOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "share.json")
	for _, mode := range []string{"write-error", "cancel-during-write", "cancel-before", "success"} {
		t.Run(mode, func(t *testing.T) {
			if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel-before" {
				cancel()
			}
			err := writeAtomic(ctx, path, func(f *os.File) error {
				if _, err := f.Write([]byte("replacement")); err != nil {
					return err
				}
				if mode == "write-error" {
					return io.ErrShortWrite
				}
				if mode == "cancel-during-write" {
					cancel()
				}
				return nil
			})
			want := "original"
			if mode == "success" {
				want = "replacement"
			}
			if (err == nil) != (mode == "success") {
				t.Fatalf("error: %v", err)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil || string(data) != want {
				t.Fatalf("target: %q %v", data, readErr)
			}
			files, _ := filepath.Glob(filepath.Join(dir, ".downloader-export-*"))
			if len(files) != 0 {
				t.Fatal("temporary file leaked")
			}
		})
	}
	if err := Write(context.Background(), filepath.Join(dir, "absent", "share.json"), []byte("new")); err == nil {
		t.Fatal("missing directory accepted")
	}
	data := []byte(strings.Repeat("x", 200000))
	if err := Write(context.Background(), path, data); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) {
		t.Fatal("chunked export truncated")
	}
}
