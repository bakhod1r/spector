package spector

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func invokeFrom(t *testing.T, h http.Handler, remote string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/docs/grpc/invoke",
		strings.NewReader(`{"target":"127.0.0.1:1","symbol":"a.B/C","data":"{}"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remote
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code
}

// The invoke endpoint dials whatever host the body names. Without an access
// key it answers only a caller on this machine: a console reachable from the
// network must not become a proxy into it.
func TestGrpcInvokeWithoutKeyIsLoopbackOnly(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir})
	if code := invokeFrom(t, h, "203.0.113.9:4444"); code != http.StatusNotFound {
		t.Errorf("remote caller without a key: status %d, want 404", code)
	}
	if code := invokeFrom(t, h, "127.0.0.1:4444"); code == http.StatusNotFound {
		t.Errorf("loopback caller was refused")
	}
	if code := invokeFrom(t, h, "[::1]:4444"); code == http.StatusNotFound {
		t.Errorf("IPv6 loopback caller was refused")
	}
}

func TestGrpcInvokeRemoteAllowedWhenOptedIn(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir, AllowRemoteGRPC: true})
	if code := invokeFrom(t, h, "203.0.113.9:4444"); code == http.StatusNotFound {
		t.Errorf("AllowRemoteGRPC did not open the endpoint")
	}
}
