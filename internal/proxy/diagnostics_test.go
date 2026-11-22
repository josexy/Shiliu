package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"go-downloader/internal/diagnostics"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestProxyFailureDiagnosticsDoNotIncludeCredentials(t *testing.T) {
	for _, kind := range []Kind{KindHTTP, KindSOCKS5} {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		host, portText, _ := net.SplitHostPort(listener.Addr().String())
		port, _ := strconv.Atoi(portText)
		listener.Close()
		log := &diagnostics.Buffer{}
		client, err := NewClientWithDiagnostics(Config{Mode: ModeManual, Kind: kind, Host: host, Port: port, Username: "proxy-user-secret", Password: "proxy-pass-secret"}, nil, log)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequestWithContext(diagnostics.WithTask(context.Background(), "proxy-job"), "GET", "http://example.invalid/file?token=query-secret", nil)
		if _, err := client.Do(req); err == nil {
			t.Fatal("closed local proxy succeeded")
		}
		client.CloseIdleConnections()
		s := log.Snapshot()
		if len(s.Events) != 1 || s.Events[0].TaskID != "proxy-job" || s.Events[0].Kind != diagnostics.ProxyFailure {
			t.Fatal(s)
		}
		data, _ := json.Marshal(s)
		if strings.Contains(string(data), "secret") {
			t.Fatal("proxy credentials leaked")
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(407)
		fmt.Fprint(w, "echo proxy-pass-secret cHJveHktcGFzcy1zZWNyZXQ=")
	}))
	defer srv.Close()
	host, port := splitHostPort(t, srv.URL)
	log := &diagnostics.Buffer{}
	client, err := NewClientWithDiagnostics(Config{Mode: ModeManual, Kind: KindHTTP, Host: host, Port: port, Password: "proxy-pass-secret"}, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://example.invalid/file")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if s := log.Snapshot(); len(s.Events) != 1 || s.Events[0].ErrorCode != "proxy_auth" || s.Events[0].HTTPStatus != 407 {
		t.Fatal(s)
	}
}
