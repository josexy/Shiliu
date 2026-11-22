package download

import (
	"encoding/json"
	"os"
)

// partMeta is saved beside a partial file so a resumed download can tell
// whether the remote file is still the one it started.
type partMeta struct {
	SchemaVersion int            `json:"schemaVersion,omitempty"`
	Mode          string         `json:"mode,omitempty"`
	ResourceURL   string         `json:"resourceUrl,omitempty"`
	Completed     []byteRange    `json:"completed,omitempty"`
	URL           string         `json:"url"`
	ETag          string         `json:"etag,omitempty"`
	LastModified  string         `json:"lastModified,omitempty"`
	Total         int64          `json:"total"`
	Resumable     bool           `json:"resumable"`
	Source        *sourceJournal `json:"source,omitempty"`
}

func readPartMeta(path string) partMeta {
	var m partMeta
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	if json.Unmarshal(data, &m) != nil {
		return partMeta{}
	}
	return m
}

func writePartMeta(path string, m partMeta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	f, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
