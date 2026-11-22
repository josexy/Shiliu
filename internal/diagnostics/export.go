package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Document struct {
	Version    int     `json:"version"`
	AppVersion string  `json:"appVersion"`
	Platform   string  `json:"platform"`
	Dropped    uint64  `json:"dropped"`
	Events     []Entry `json:"events"`
}

func Export(ctx context.Context, path, appVersion string, snapshot Snapshot) error {
	if len(appVersion) > 64 || strings.IndexFunc(appVersion, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' || r == '+')
	}) >= 0 {
		appVersion = "unknown"
	}
	if snapshot.Events == nil {
		snapshot.Events = []Entry{}
	}
	data, err := json.MarshalIndent(Document{1, appVersion, runtime.GOOS + "/" + runtime.GOARCH, snapshot.Dropped, snapshot.Events}, "", "  ")
	if err != nil {
		return fmt.Errorf("编码诊断日志：%w", err)
	}
	return exportAtomic(ctx, path, func(f *os.File) error {
		for len(data) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := f.Write(data[:min(len(data), 64*1024)])
			if err != nil {
				return err
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			data = data[n:]
		}
		return nil
	})
}
func exportAtomic(ctx context.Context, path string, write func(*os.File) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".diagnostics-*")
	if err != nil {
		return fmt.Errorf("创建诊断日志临时文件：%w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := write(f); err != nil {
		return fmt.Errorf("写入诊断日志：%w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("替换诊断日志：%w", err)
	}
	return nil
}
