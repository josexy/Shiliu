package tasklist

import (
	"encoding/json"

	"go-downloader/internal/download"
)

// List is a versioned sharing format, NOT a backup. Sanitized URLs may lack
// signatures required to download. Importing this JSON is intentionally absent.
type List struct {
	Version int          `json:"version"`
	Tasks   []ExportTask `json:"tasks"`
}

// ExportTask is an explicit allowlist; never serialize download.Record here.
type ExportTask struct {
	FileName string         `json:"fileName"`
	URL      string         `json:"url"`
	Total    int64          `json:"total"`
	Done     int64          `json:"done"`
	State    download.State `json:"state"`
}

// Export exports all items when selected is nil, otherwise only selected IDs.
// Unknown sizes remain -1, rather than being confused with empty files.
func Export(items []download.Item, selected map[string]bool) ([]byte, error) {
	list := List{Version: 1, Tasks: make([]ExportTask, 0, len(items))}
	for _, it := range items {
		if selected != nil && !selected[it.ID] {
			continue
		}
		list.Tasks = append(list.Tasks, ExportTask{
			FileName: download.Sanitize(it.FileName), URL: download.DiagnosticURL(it.URL),
			Total: it.Total, Done: it.Done, State: it.State,
		})
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
