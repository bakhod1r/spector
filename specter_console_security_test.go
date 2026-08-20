package specter

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// collidingSrc documents an API whose own paths end exactly like the console's
// endpoints. Nothing stops a real service from having a /source or an
// openapi.json of its own, and the console must not answer for them.
const collidingSrc = `package app

import "github.com/gin-gonic/gin"

type Doc struct {
	ID string ` + "`json:\"id\"`" + `
}

func Register(r *gin.Engine) {
	r.GET("/v1/documents/:id/source", func(c *gin.Context) { c.JSON(200, Doc{}) })
	r.GET("/v1/exports/openapi.json", func(c *gin.Context) { c.JSON(200, Doc{}) })
	r.GET("/v1/widgets", func(c *gin.Context) { c.JSON(200, Doc{}) })
}
`

// A documented path that ends in one of the console's endpoint names belongs to
// the API. Matching by suffix handed /v1/documents/{id}/source to the source
// reader and served the entire specification at /v1/exports/openapi.json.
func TestHandlerDoesNotClaimDocumentedPathsBySuffix(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": collidingSrc})
	h := Handler(Config{Dir: dir, Mock: true})

	for _, path := range []string{"/v1/documents/abc/source", "/v1/exports/openapi.json"} {
		w := get(t, h, path)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 from the mock", path, w.Code)
		}
		if strings.Contains(w.Body.String(), "\"openapi\"") {
			t.Errorf("%s: served the specification instead of the mocked route", path)
		}
	}

	// The console's own endpoints still answer at the mount point.
	if w := get(t, h, "/docs/openapi.json"); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), "\"openapi\"") {
		t.Errorf("console openapi.json = %d %q", w.Code, w.Body.String())
	}
}

// The gRPC endpoints dial a host the request names. In production without a key
// that is server-side request forgery, reachable by anyone who can reach the
// console.
func TestProductionClosesGrpcInvokeWithoutKey(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir, Production: true})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/docs/grpc/invoke",
		strings.NewReader(`{"target":"127.0.0.1:1","symbol":"a/B","data":"{}"}`)))
	if w.Code != http.StatusNotFound {
		t.Errorf("invoke status = %d, want 404 (body: %s)", w.Code, w.Body.String())
	}

	if got := get(t, h, "/docs/grpc/stream").Code; got != http.StatusNotFound {
		t.Errorf("stream status = %d, want 404", got)
	}
}

// With a key, production keeps the endpoint: the deployment has said who may
// use it. Anything other than 404 proves the request reached the handler.
func TestProductionKeepsGrpcInvokeBehindKey(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir, Production: true, AccessKey: "s3cret"})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/docs/grpc/invoke?key=s3cret",
		strings.NewReader(`{"target":"127.0.0.1:1","symbol":"a/B","data":"{}"}`))
	h.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Errorf("status = 404 with the key; want the endpoint to exist")
	}
}

// The key is a deployment secret. A cookie scoped to "/" rides along on every
// request the application serves, where any header-logging middleware records
// it.
func TestAccessCookieIsScopedToTheConsole(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir, AccessKey: "s3cret"})

	w := get(t, h, "/docs/?key=s3cret")
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v, want one", cookies)
	}
	if cookies[0].Path != "/docs" {
		t.Errorf("cookie path = %q, want /docs", cookies[0].Path)
	}
	if cookies[0].Secure {
		t.Errorf("cookie marked Secure over plain HTTP")
	}
}

// TLS is usually terminated by a proxy, so r.TLS is nil on exactly the
// deployments the browser reached over HTTPS.
func TestAccessCookieIsSecureBehindHTTPSProxy(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir, AccessKey: "s3cret"})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/?key=s3cret", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Errorf("cookies = %v, want one marked Secure", cookies)
	}
}

// A tool that reads source must show the source as it is. Caching the first
// scan for the life of the process means an edit needs a restart.
func TestHandlerRescansAfterAnEdit(t *testing.T) {
	noRecheckDelay(t)
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir})

	if body := get(t, h, "/docs/openapi.json").Body.String(); !strings.Contains(body, "/widgets") {
		t.Fatalf("first scan missing /widgets: %s", body)
	}

	edited := strings.Replace(ginSrc, `r.GET("/widgets"`, `r.GET("/gadgets"`, 1)
	rewrite(t, filepath.Join(dir, "main.go"), edited)

	body := get(t, h, "/docs/openapi.json").Body.String()
	if !strings.Contains(body, "/gadgets") {
		t.Errorf("edit not picked up: %s", body)
	}
}

// A scan that failed on a half-written file must not keep answering 500 after
// the file is fixed.
func TestHandlerRecoversFromABrokenTree(t *testing.T) {
	noRecheckDelay(t)
	dir := writeTree(t, map[string]string{"main.go": "package {\n"})
	h := Handler(Config{Dir: dir})

	if got := get(t, h, "/docs/openapi.json").Code; got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for a tree that does not parse", got)
	}

	rewrite(t, filepath.Join(dir, "main.go"), ginSrc)

	w := get(t, h, "/docs/openapi.json")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d after the fix, want 200 (%s)", w.Code, w.Body.String())
	}
}

// noRecheckDelay makes the console fingerprint the tree on every request, so a
// test does not have to sleep out the throttle.
func noRecheckDelay(t *testing.T) {
	t.Helper()
	old := consoleRecheck
	consoleRecheck = 0
	t.Cleanup(func() { consoleRecheck = old })
}

// rewrite replaces a file's contents and moves its modification time forward,
// so the rescan sees the change however fast the test runs.
func rewrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}
