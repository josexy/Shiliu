package tasklist

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go-downloader/internal/download"
)

// Read performs bounded file I/O. Call on a worker, then publish the preview on
// the UI thread. Cancellation is checked between regular-file reads.
func Read(ctx context.Context, path string, existing []download.Item) (Preview, error) {
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Preview{}, fmt.Errorf("读取 TXT 失败：%w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Preview{}, err
	}
	if !info.Mode().IsRegular() {
		return Preview{}, fmt.Errorf("请选择普通 TXT 文件")
	}
	data, err := readBounded(ctx, f)
	if err != nil {
		return Preview{}, err
	}
	return Parse(ctx, data, existing)
}

func readBounded(ctx context.Context, r io.Reader) ([]byte, error) {
	data := make([]byte, 0, 64*1024)
	buffer := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := r.Read(buffer[:min(len(buffer), MaxBytes+1-len(data))])
		data = append(data, buffer[:n]...)
		if len(data) > MaxBytes {
			return nil, fmt.Errorf("TXT 文件不能超过 10 MiB")
		}
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			return nil, fmt.Errorf("读取 TXT 失败：%w", err)
		}
	}
}

// Write saves a complete sharing list through a private same-directory file.
// Cancellation before commit preserves the old target. A successful rename is
// the commit point; cancellation after it does not turn success into failure.
// This is not a backup and never writes the manager's persisted record format.
func Write(ctx context.Context, path string, data []byte) error {
	return writeAtomic(ctx, path, func(f *os.File) error {
		for len(data) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			chunk := data[:min(len(data), 64*1024)]
			n, err := f.Write(chunk)
			if err != nil {
				return err
			}
			if n != len(chunk) {
				return io.ErrShortWrite
			}
			data = data[n:]
		}
		return nil
	})
}

func writeAtomic(ctx context.Context, path string, write func(*os.File) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".downloader-export-*")
	if err != nil {
		return fmt.Errorf("创建导出文件失败：%w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := write(f); err != nil {
		return fmt.Errorf("写入导出文件失败：%w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("保存导出文件失败：%w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("替换导出文件失败：%w", err)
	}
	return nil
}
