package specter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// With an access key configured, an unauthenticated WebSocket upgrade to
// grpc/stream must be refused (the console 404s rather than confirming it is
// there).
func TestGrpcStreamUpgradeRequiresKey(t *testing.T) {
	h := Handler(Config{Dir: "examples/shop", AccessKey: "secret"})
	srv := httptest.NewServer(h)
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/docs/grpc/stream"
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatal("expected upgrade to be refused without the key")
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Errorf("expected 404, got %d", code)
	}
}

// With the correct key, the grpc/stream route must exist and complete the
// WebSocket handshake (101 Switching Protocols), proving the route is wired
// and the auth gate lets authorized clients through.
func TestGrpcStreamUpgradeSucceedsWithKey(t *testing.T) {
	h := Handler(Config{Dir: "examples/shop", AccessKey: "secret"})
	srv := httptest.NewServer(h)
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/docs/grpc/stream?key=secret"
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("expected upgrade to succeed with key, got err: %v", err)
	}
	defer conn.Close()
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Errorf("expected 101 Switching Protocols, got %d", code)
	}
}
