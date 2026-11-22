// Package rules chooses a new task's directory without I/O or UI dependencies.
package rules

import (
	"net/url"
	"path/filepath"
	"strings"
)

// Select gives explicit overrides precedence, then the first enabled match in
// list order (neither condition kind has implicit priority), then the default.
// Callers pass the initial chosen name, never a later response filename.
func Select(list []Rule, rawURL, fileName, defaultDir, override string) (dir, ruleID string) {
	if override != "" {
		return override, ""
	}
	host := ""
	if u, err := url.Parse(rawURL); err == nil {
		host, _ = NormalizeHost(u.Hostname())
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	for _, r := range list {
		if !r.Enabled {
			continue
		}
		match := false
		switch r.Kind {
		case "extension":
			match = ext != "" && ext == r.Pattern
		case "domain":
			if strings.HasPrefix(r.Pattern, "*.") {
				match = host != "" && strings.HasSuffix(host, r.Pattern[1:])
			} else {
				match = host != "" && host == r.Pattern
			}
		}
		if match {
			return r.Dir, r.ID
		}
	}
	return defaultDir, ""
}
