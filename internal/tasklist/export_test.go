package tasklist

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"go-downloader/internal/download"
)

func TestExportGoldenAndSelection(t *testing.T) {
	items := []download.Item{
		{ID: "one", FileName: "file.bin", URL: "https://private-user:private-pass@example.com/file.bin?signature=private-query#private-fragment", Path: "C:/private-path/file.bin", Total: -1, Done: 12, State: download.StatePaused, Error: "private-error", NeedsHeaders: true},
		{ID: "two", FileName: "empty.txt", URL: "https://example.com/empty.txt", Total: 0, State: download.StateCompleted},
	}
	data, err := Export(items, nil)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/share-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strings.ReplaceAll(string(golden), "\r\n", "\n") {
		t.Fatalf("unexpected share format:\n%s", data)
	}
	for _, forbidden := range []string{"private-", "path", "headers", "proxy", "signature", "error", "id"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("export contains %q", forbidden)
		}
	}
	for _, selected := range []map[string]bool{{"two": true}, {}} {
		data, err := Export(items, selected)
		if err != nil {
			t.Fatal(err)
		}
		var list List
		if err := json.Unmarshal(data, &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Tasks) != len(selected) {
			t.Fatal("selection not respected")
		}
		if len(list.Tasks) > 0 && list.Tasks[0].FileName != "empty.txt" {
			t.Fatal("wrong selected task")
		}
	}
}
