package download

import "go-downloader/internal/diagnostics"

func (m *Manager) setStateLocked(t *task, state State) {
	if t.item.State == state {
		return
	}
	t.item.State = state
	m.recordStateLocked(t)
}
func (m *Manager) recordStateLocked(t *task) {
	if m.diagnosticPending != nil {
		m.diagnosticPending.Record(diagnostics.NewEvent(diagnostics.Kind(t.item.State), t.item.ID, t.item.URL, 0, t.item.RetryCount, nil))
	}
}
func (m *Manager) diagnostic(t *task, kind diagnostics.Kind, status int, err error) {
	if m.diagnosticPending == nil {
		return
	}
	m.mu.Lock()
	m.diagnosticPending.Record(diagnostics.NewEvent(kind, t.item.ID, t.item.URL, status, t.item.RetryCount, err))
	m.mu.Unlock()
	m.flushDiagnostics()
}
func (m *Manager) flushDiagnostics() {
	if m.diagnosticPending == nil {
		return
	}
	m.mu.Lock()
	events, dropped := m.diagnosticPending.Drain()
	sink := m.opts.Diagnostics
	m.mu.Unlock()
	if dropped > 0 {
		sink.Omit(dropped)
	}
	for _, event := range events {
		sink.Record(event)
	}
}
