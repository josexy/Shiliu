package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"time"
)

const verificationChunkSize = 256 * 1024

func sameVerifiedFile(before, after os.FileInfo) bool {
	return before != nil && after != nil && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && after.Mode().IsRegular()
}

// verifyFileSHA256 checks identity, size and modification time around reading.
// This describes the observed file at check time, not protection against an
// adversary concurrently rewriting bytes and restoring timestamps afterwards.
func verifyFileSHA256(ctx context.Context, path, expected string, progress func(int64, int64)) (result VerificationResult, identity os.FileInfo) {
	result = VerificationResult{State: VerificationFailed, ExpectedSHA256: expected}
	defer func() { result.CheckedAt = time.Now() }()
	if ctx.Err() != nil {
		result.State = VerificationCanceled
		return
	}
	f, err := os.Open(path)
	if err != nil {
		result.Error = "文件不存在或无法读取，请检查路径和权限"
		return
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() {
		result.Error = "无法校验此文件"
		return
	}
	result.Size, result.ModifiedAt = before.Size(), before.ModTime()
	progress(0, before.Size())
	hash := sha256.New()
	buffer := make([]byte, verificationChunkSize)
	var done int64
	for {
		if ctx.Err() != nil {
			result.State = VerificationCanceled
			return
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			hash.Write(buffer[:n])
			done += int64(n)
			progress(done, before.Size())
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			result.Error = "读取文件内容失败"
			return
		}
	}
	if ctx.Err() != nil {
		result.State = VerificationCanceled
		return
	}
	after, err := f.Stat()
	if err != nil || !sameVerifiedFile(before, after) || done != before.Size() {
		result.Error = "文件在校验期间发生变化"
		return
	}
	if err := f.Close(); err != nil {
		result.Error = "关闭校验文件失败"
		return
	}
	current, err := os.Stat(path)
	if err != nil || !sameVerifiedFile(before, current) {
		result.Error = "文件在校验期间被替换或删除"
		return
	}
	result.ActualSHA256 = hex.EncodeToString(hash.Sum(nil))
	result.State = VerificationMismatched
	if result.ActualSHA256 == expected {
		result.State = VerificationMatched
	}
	return result, before
}
