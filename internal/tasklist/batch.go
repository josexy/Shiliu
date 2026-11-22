package tasklist

import (
	"bytes"
	"context"
	"fmt"
	"go-downloader/internal/download"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ReadBatch reads only regular TXT files, and applies the import limits to the
// combined batch. Text blocks are owned copies from the native drop event.
func ReadBatch(ctx context.Context, texts [][]byte, paths []string, existing []download.Item) (Preview, error) {
	if len(texts)+len(paths) > MaxCandidates {
		return Preview{}, fmt.Errorf("一次最多处理 10000 个投放项目")
	}
	var combined []byte
	appendText := func(data []byte) error {
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return fmt.Errorf("仅支持 UTF-8 TXT 或链接文本，不支持二进制内容")
		}
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		if len(combined)+len(data)+1 > MaxBytes {
			return fmt.Errorf("合并后的投放不能超过 10 MiB")
		}
		combined = append(combined, data...)
		combined = append(combined, '\n')
		return nil
	}
	for _, data := range texts {
		if err := ctx.Err(); err != nil {
			return Preview{}, err
		}
		if err := appendText(data); err != nil {
			return Preview{}, err
		}
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return Preview{}, err
		}
		if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".txt") {
			return Preview{}, fmt.Errorf("仅支持本地 TXT 普通文件；请使用粘贴或导入 TXT")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return Preview{}, fmt.Errorf("无法读取投放项目：仅支持可读的普通 TXT 文件，不支持目录或符号链接")
		}
		f, err := os.Open(path)
		if err != nil {
			return Preview{}, fmt.Errorf("无法读取 TXT，请检查文件权限")
		}
		opened, statErr := f.Stat()
		if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			f.Close()
			return Preview{}, fmt.Errorf("投放文件在读取前发生变化")
		}
		data, err := readBounded(ctx, io.LimitReader(f, int64(MaxBytes-len(combined)+1)))
		f.Close()
		if err != nil {
			return Preview{}, err
		}
		if err := appendText(data); err != nil {
			return Preview{}, err
		}
	}
	return Parse(ctx, combined, existing)
}
