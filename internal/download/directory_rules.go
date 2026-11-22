package download

import "go-downloader/internal/rules"

func (m *Manager) SetDirectoryRules(input []rules.Rule) error {
	normalized, err := rules.Normalize(input)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.directoryRules = normalized
	m.mu.Unlock()
	return nil
}
