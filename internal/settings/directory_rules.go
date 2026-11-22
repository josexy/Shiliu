package settings

import "go-downloader/internal/rules"

// DirectoryRule keeps the persisted settings API while sharing a dependency-free
// rule model with the download engine (settings also contains proxy config).
type DirectoryRule = rules.Rule

func NormalizeRuleHost(raw string) (string, error) { return rules.NormalizeHost(raw) }
func NormalizeDirectoryRules(input []DirectoryRule) ([]DirectoryRule, error) {
	return rules.Normalize(input)
}
