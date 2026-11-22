// Package diagnostics holds only sanitized, bounded events for this process.
// It does not collect headers, bodies, arbitrary error strings, or clipboard data.
package diagnostics

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	MaxEvents     = 1000
	MaxBytes      = 1024 * 1024
	MaxEventBytes = 2048
)

type Kind string

const (
	Queued       Kind = "queued"
	Connecting   Kind = "connecting"
	Downloading  Kind = "downloading"
	Paused       Kind = "paused"
	Completed    Kind = "completed"
	Failed       Kind = "failed"
	Removed      Kind = "removed"
	Retry        Kind = "retry"
	HTTP         Kind = "http"
	ProxyFailure Kind = "proxy_failure"
)

type Entry struct {
	Timestamp         time.Time `json:"timestamp"`
	TaskID            string    `json:"taskID,omitempty"`
	Kind              Kind      `json:"kind"`
	HTTPStatus        int       `json:"httpStatus,omitempty"`
	Retry             int       `json:"retry,omitempty"`
	SourceHostAndPath string    `json:"sourceHostAndPath,omitempty"`
	ErrorCode         string    `json:"errorCode,omitempty"`
}

// Event can only be populated by NewEvent, so a Sink never receives raw input.
type Event struct {
	entry Entry
	size  int
}
type Sink interface {
	Record(Event)
	Omit(uint64)
}

func NewEvent(kind Kind, taskID, source string, status, retry int, err error) Event {
	switch kind {
	case Queued, Connecting, Downloading, Paused, Completed, Failed, Removed, Retry, HTTP, ProxyFailure:
	default:
		kind = "unknown"
	}
	if len(taskID) > 64 || strings.IndexFunc(taskID, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.')
	}) >= 0 {
		taskID = ""
	}
	if status < 100 || status > 599 {
		status = 0
	}
	e := Entry{Timestamp: time.Now().UTC(), TaskID: taskID, Kind: kind, HTTPStatus: status, Retry: max(0, retry), ErrorCode: ErrorCode(err)}
	if status == 407 && kind == ProxyFailure {
		e.ErrorCode = "proxy_auth"
	}
	if len(source) <= MaxEventBytes && utf8.ValidString(source) {
		if u, parseErr := url.Parse(source); parseErr == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			e.SourceHostAndPath = u.Host + u.EscapedPath()
		}
	}
	data, _ := json.Marshal(e)
	if len(data) > MaxEventBytes {
		e.SourceHostAndPath = ""
		data, _ = json.Marshal(e)
	}
	return Event{entry: e, size: len(data)}
}

func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	var cert *tls.CertificateVerificationError
	if errors.As(err, &cert) {
		return "tls"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "timeout"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection_refused"
	}
	if errors.Is(err, os.ErrPermission) {
		return "permission"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "not_found"
	}
	return "unknown"
}

type taskKey struct{}

func WithTask(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, taskKey{}, id)
}
func TaskID(ctx context.Context) string { id, _ := ctx.Value(taskKey{}).(string); return id }
