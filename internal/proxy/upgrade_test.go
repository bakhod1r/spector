package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// httputil.ReverseProxy hijacks the client connection for a 101 upgrade
// through http.NewResponseController, which finds the Hijacker by unwrapping.
// A wrapper without Unwrap made every WebSocket through -proxy fail.
func TestCaptureUnwrapsToTheHijacker(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &capture{ResponseWriter: rec}
	var w http.ResponseWriter = c
	u, ok := w.(interface{ Unwrap() http.ResponseWriter })
	if !ok {
		t.Fatal("capture has no Unwrap; ResponseController cannot reach Hijack")
	}
	if u.Unwrap() != rec {
		t.Fatal("Unwrap does not return the wrapped writer")
	}
}
