package diagnostics

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestEventsRedactBeforeBuffer(t *testing.T) {
	secrets := []string{"user-secret", "proxy-pass", "cookie-secret", "bearer-secret"}
	for _, secret := range secrets {
		for _, echo := range []string{secret, url.QueryEscape(secret), base64.StdEncoding.EncodeToString([]byte(secret))} {
			b := &Buffer{}
			b.Record(NewEvent(ProxyFailure, "task-1", "https://user-secret:proxy-pass@example.com/file?token="+echo+"#"+echo, 407, 2, fmt.Errorf("Cookie: %s Authorization: %s C:\\private\\%s", echo, echo, echo)))
			encoded, _ := json.Marshal(b.Snapshot())
			for _, s := range append(secrets, echo, "C:\\private") {
				if strings.Contains(string(encoded), s) {
					t.Fatalf("cached sensitive field: %s", s)
				}
			}
			if b.Snapshot().Events[0].SourceHostAndPath != "example.com/file" {
				t.Fatal("source not minimized")
			}
		}
	}
	b := &Buffer{}
	b.Record(NewEvent(Failed, "task-2", "C:\\Users\\secret\\file", 0, 0, fmt.Errorf("unknown credential %s", "private")))
	s := b.Snapshot()
	if s.Events[0].SourceHostAndPath != "" || s.Events[0].ErrorCode != "unknown" {
		t.Fatal(s)
	}
}

func TestBufferCountByteUTF8AndConcurrentSnapshots(t *testing.T) {
	b := &Buffer{}
	for i := 0; i < MaxEvents+20; i++ {
		b.Record(NewEvent(HTTP, fmt.Sprint(i), "https://example.com/a", 503, i, nil))
	}
	s := b.Snapshot()
	if len(s.Events) != MaxEvents || s.Dropped != 20 || s.Events[0].TaskID != "20" {
		t.Fatal("count limit", len(s.Events), s.Dropped)
	}
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				b.Record(NewEvent(HTTP, "task", "https://example.com/"+strings.Repeat("x", 1700), 503, i, nil))
				data, err := json.Marshal(b.Snapshot())
				if err != nil || !utf8.Valid(data) {
					t.Error("invalid snapshot")
				}
			}
		}()
	}
	wg.Wait()
	b.mu.Lock()
	if b.bytes > MaxBytes || b.count > MaxEvents {
		t.Error("capacity exceeded")
	}
	b.mu.Unlock()
	large := NewEvent(Failed, "task", "https://example.com/"+strings.Repeat("汉", 2000), 0, 0, nil)
	if large.size > MaxEventBytes || large.entry.SourceHostAndPath != "" {
		t.Fatal("long source not omitted")
	}
	frozen := b.Snapshot()
	b.Record(NewEvent(Completed, "new", "", 0, 0, nil))
	if len(frozen.Events) == 0 || frozen.Events[len(frozen.Events)-1].TaskID == "new" {
		t.Fatal("snapshot changed after capture")
	}
}
