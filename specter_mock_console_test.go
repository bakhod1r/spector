package specter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// When Config.Mock is set, the console Handler serves the documented API from
// its own origin: a documented path answers with mock JSON instead of falling
// through to the console page's HTML. This is what makes the console's "Send"
// button return a real body during a demo, without a second process or CORS.
func TestConsoleMockServesJSON(t *testing.T) {
	h := Handler(Config{Dir: "examples/shop", Title: "Shop", Version: "1.0.0", Mock: true})

	// A documented path returns JSON from the mock, not the console HTML.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/categories", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("documented path: got status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("documented path Content-Type = %q, want application/json", ct)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("documented path returned the console HTML, want mock JSON")
	}

	// The console page itself still loads at its mount point.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("console path Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "<html") {
		t.Error("console path did not return the page")
	}
	// The page announces it is in mock mode so the header can show the badge.
	if !strings.Contains(rec.Body.String(), "window.__MOCK__=true") {
		t.Error("console page did not have the mock flag set")
	}
}

// Without Mock, the Handler keeps its zero-config behaviour: an unknown path
// falls through to the console page (a single-page app catch-all).
func TestConsoleNoMockUnchanged(t *testing.T) {
	h := Handler(Config{Dir: "examples/shop", Title: "Shop", Version: "1.0.0"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/categories", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("without Mock, Content-Type = %q, want text/html (unchanged)", ct)
	}
	body := readAll(t, h, "/docs/")
	if strings.Contains(body, "window.__MOCK__=true") {
		t.Error("without Mock, the page should not claim mock mode")
	}
}

func readAll(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Body.String()
}
