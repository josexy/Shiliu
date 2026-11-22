package proxy

import (
	"go-downloader/internal/diagnostics"
	"net"
	"net/http"
)

// NewClientWithDiagnostics observes failures on a selected proxy route, without
// exporting endpoints, credentials, headers, response bodies, or raw errors.
func NewClientWithDiagnostics(c Config, sys *System, sink diagnostics.Sink) (*http.Client, error) {
	client, err := NewClient(c, sys)
	if err != nil || sink == nil {
		return client, err
	}
	c.Normalize()
	base := client.Transport.(*http.Transport)
	socks, bypass := false, c.Bypass
	if c.Mode == ModeManual {
		socks = c.Kind == KindSOCKS5
	} else if c.Mode == ModeSystem && sys != nil {
		socks = sys.HTTP == nil && sys.HTTPS == nil && sys.SOCKS != nil
		bypass = sys.Bypass
	}
	client.Transport = &diagnosticTransport{base: base, sink: sink, socks: socks, bypass: bypass}
	return client, nil
}

type diagnosticTransport struct {
	base   *http.Transport
	sink   diagnostics.Sink
	socks  bool
	bypass string
}

func (t *diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	proxied := false
	if t.base.Proxy != nil {
		ep, err := t.base.Proxy(r)
		proxied = err == nil && ep != nil
	}
	if t.socks {
		port := r.URL.Port()
		if port == "" {
			port = "80"
			if r.URL.Scheme == "https" {
				port = "443"
			}
		}
		proxied = !MatchesBypass(net.JoinHostPort(r.URL.Hostname(), port), t.bypass)
	}
	response, err := t.base.RoundTrip(r)
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	if proxied && (err != nil && r.Context().Err() == nil || status == http.StatusProxyAuthRequired) {
		t.sink.Record(diagnostics.NewEvent(diagnostics.ProxyFailure, diagnostics.TaskID(r.Context()), r.URL.String(), status, 0, err))
	}
	return response, err
}
func (t *diagnosticTransport) CloseIdleConnections() { t.base.CloseIdleConnections() }
