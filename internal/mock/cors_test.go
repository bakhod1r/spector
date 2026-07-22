package mock

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

// callWith issues a request carrying an Origin header, which is what makes it a
// CORS request as far as the handler is concerned.
func callWith(t *testing.T, opts Options, method, origin string) *httptest.ResponseRecorder {
	t.Helper()
	doc := docWith(map[string]*core.Schema{
		"T": {Type: "object", Properties: map[string]*core.Schema{"id": {Type: "integer"}}},
	}, "/things", "get", okOp("T"))

	req := httptest.NewRequest(method, "/things", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	HandlerWith(doc, opts).ServeHTTP(w, req)
	return w
}

// The default has to be open: the mock exists to be called from wherever the
// frontend happens to be running, and a dev server's port changes.
func TestDefaultAllowsAnyOrigin(t *testing.T) {
	w := callWith(t, DefaultOptions(), http.MethodGet, "https://app.example.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("= %q, want *", got)
	}
}

func TestPreflightIsAnsweredWithTheMethodsAndHeaders(t *testing.T) {
	w := callWith(t, DefaultOptions(), http.MethodOptions, "https://app.example.com")
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if m := w.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(m, "POST") {
		t.Errorf("methods = %q", m)
	}
	if h := w.Header().Get("Access-Control-Allow-Headers"); h == "" {
		t.Error("no allowed headers on the preflight")
	}
}

// ---- restricting origins ----

func TestListedOriginIsEchoed(t *testing.T) {
	opts := Options{AllowOrigins: []string{"https://app.example.com"}}
	w := callWith(t, opts, http.MethodGet, "https://app.example.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("= %q, want the origin echoed", got)
	}
}

// An origin that is not listed gets no CORS header at all — that absence is
// what makes the browser block the response. The request is still answered,
// because CORS is enforced by the browser and pretending otherwise would give
// a false picture of how the real API behaves.
func TestUnlistedOriginGetsNoHeader(t *testing.T) {
	opts := Options{AllowOrigins: []string{"https://app.example.com"}}
	w := callWith(t, opts, http.MethodGet, "https://evil.example.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("= %q, want no header for an unlisted origin", got)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d; the server should still answer", w.Code)
	}
}

// Whenever the header depends on the request's Origin, a cache that ignores
// that would serve one origin's response to another.
func TestVaryIsSetWhenTheOriginDecides(t *testing.T) {
	opts := Options{AllowOrigins: []string{"https://app.example.com"}}
	w := callWith(t, opts, http.MethodGet, "https://app.example.com")
	if v := w.Header().Get("Vary"); !strings.Contains(v, "Origin") {
		t.Errorf("Vary = %q, want it to include Origin", v)
	}
}

// A wildcard response is the same for everyone, so there is nothing to vary on.
func TestVaryIsNotSetForAWildcard(t *testing.T) {
	w := callWith(t, DefaultOptions(), http.MethodGet, "https://app.example.com")
	if v := w.Header().Get("Vary"); strings.Contains(v, "Origin") {
		t.Errorf("Vary = %q, want no Origin for a wildcard", v)
	}
}

// A request with no Origin is not a CORS request — curl, a test, a server-side
// client — and must not be treated as a rejected one.
func TestNonBrowserRequestIsUnaffected(t *testing.T) {
	opts := Options{AllowOrigins: []string{"https://app.example.com"}}
	w := callWith(t, opts, http.MethodGet, "")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ---- credentials ----

// This is the rule that makes credentials more than a boolean: browsers reject
// "*" outright when credentials are requested, so a mock that sent both would
// be unusable for exactly the case the option exists for.
func TestCredentialsNeverPairWithAWildcard(t *testing.T) {
	opts := Options{AllowCredentials: true} // no origins: would otherwise be "*"
	w := callWith(t, opts, http.MethodGet, "https://app.example.com")

	origin := w.Header().Get("Access-Control-Allow-Origin")
	if origin == "*" {
		t.Fatal("wildcard sent alongside credentials; every browser rejects this")
	}
	if origin != "https://app.example.com" {
		t.Errorf("= %q, want the caller's origin echoed", origin)
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("credentials were requested but not allowed")
	}
}

func TestCredentialsWithAListedOrigin(t *testing.T) {
	opts := Options{AllowOrigins: []string{"https://app.example.com"}, AllowCredentials: true}
	w := callWith(t, opts, http.MethodGet, "https://app.example.com")
	if w.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Errorf("origin = %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("credentials not allowed")
	}
}

// Echoing an origin only stays safe while it is restricted; with credentials on
// and a list configured, an unlisted origin must still be refused.
func TestCredentialsDoNotBypassTheOriginList(t *testing.T) {
	opts := Options{AllowOrigins: []string{"https://app.example.com"}, AllowCredentials: true}
	w := callWith(t, opts, http.MethodGet, "https://evil.example.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("= %q, want nothing for an unlisted origin", got)
	}
}

func TestCredentialsOffSendsNoCredentialHeader(t *testing.T) {
	w := callWith(t, DefaultOptions(), http.MethodGet, "https://app.example.com")
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Error("credentials header sent when not enabled")
	}
}

// ---- the rest of the policy ----

func TestCustomMethodsAndHeaders(t *testing.T) {
	opts := Options{
		AllowMethods: []string{"GET", "POST"},
		AllowHeaders: []string{"Authorization", "Content-Type"},
	}
	w := callWith(t, opts, http.MethodOptions, "https://app.example.com")

	if m := w.Header().Get("Access-Control-Allow-Methods"); m != "GET,POST" {
		t.Errorf("methods = %q", m)
	}
	if h := w.Header().Get("Access-Control-Allow-Headers"); h != "Authorization,Content-Type" {
		t.Errorf("headers = %q", h)
	}
}

func TestMaxAge(t *testing.T) {
	w := callWith(t, Options{MaxAge: 600}, http.MethodOptions, "https://app.example.com")
	if got := w.Header().Get("Access-Control-Max-Age"); got != "600" {
		t.Errorf("= %q, want 600", got)
	}
}

func TestMaxAgeUnsetByDefault(t *testing.T) {
	w := callWith(t, DefaultOptions(), http.MethodOptions, "https://app.example.com")
	if got := w.Header().Get("Access-Control-Max-Age"); got != "" {
		t.Errorf("= %q, want the browser's own default", got)
	}
}

// An explicit "*" in the list means the same as an empty list.
func TestExplicitWildcardInTheList(t *testing.T) {
	w := callWith(t, Options{AllowOrigins: []string{"*"}}, http.MethodGet, "https://app.example.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("= %q, want *", got)
	}
}

// Origins are compared case-insensitively on the scheme and host, which is how
// browsers send them and how they are usually written in config.
func TestOriginMatchIsCaseInsensitive(t *testing.T) {
	opts := Options{AllowOrigins: []string{"https://App.Example.com"}}
	w := callWith(t, opts, http.MethodGet, "https://app.example.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got == "" {
		t.Error("a case difference rejected an allowed origin")
	}
}
