package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/user/specter"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

// freshRouter gives each test its own seeded in-memory store, so one test's
// writes and deletes cannot change what another test sees. A shared store made
// these tests order-dependent — a DELETE in one turned a GET in the next into a
// 404 — which is exactly the kind of flake a fresh database per test removes.
func freshRouter(t *testing.T) *gin.Engine {
	t.Helper()
	var err error
	if db, err = openStore(":memory:"); err != nil {
		t.Fatal(err)
	}
	return router()
}

// apiRoutes returns only this example's own endpoints. The /docs console is
// mounted by mount and is covered by that package's tests.
func apiRoutes(r *gin.Engine) []gin.RouteInfo {
	var out []gin.RouteInfo
	for _, rt := range r.Routes() {
		if strings.HasPrefix(rt.Path, "/api/") {
			out = append(out, rt)
		}
	}
	return out
}

func do(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Every registered route must answer. Walking gin's own route table means a
// route added later is exercised without touching this test.
func TestEveryRouteResponds(t *testing.T) {
	r := freshRouter(t)
	routes := apiRoutes(r)
	if len(routes) == 0 {
		t.Fatal("no routes registered")
	}

	for _, rt := range routes {
		t.Run(rt.Method+" "+rt.Path, func(t *testing.T) {
			// Path params are placeholders in the route table; any value works
			// since the handlers do not look them up.
			path := strings.ReplaceAll(rt.Path, ":id", "1")

			body := ""
			if rt.Method == http.MethodPost || rt.Method == http.MethodPut {
				body = "{}"
			}
			w := do(t, r, rt.Method, path, body)

			if w.Code >= 500 {
				t.Errorf("status = %d, want a non-server-error response; body: %s", w.Code, w.Body.String())
			}
			if w.Body.Len() > 0 && !json.Valid(w.Body.Bytes()) {
				t.Errorf("response is not valid JSON: %s", w.Body.String())
			}
		})
	}
}

// Writes accept a JSON body and answer 200/201.
func TestWritesAcceptValidJSON(t *testing.T) {
	r := freshRouter(t)
	for _, rt := range apiRoutes(r) {
		if rt.Method != http.MethodPost && rt.Method != http.MethodPut {
			continue
		}
		t.Run(rt.Method+" "+rt.Path, func(t *testing.T) {
			path := strings.ReplaceAll(rt.Path, ":id", "1")
			w := do(t, r, rt.Method, path, "{}")

			// An empty object is only a valid body for a route that requires
			// nothing. Where binding tags declare required fields, gin rejects
			// it — and the generated document says so too, which is the whole
			// claim: the schema and the running service agree.
			if requiresFields(t, rt.Path) {
				if w.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400 for an empty body on a route with required fields; body: %s",
						w.Code, w.Body.String())
				}
				return
			}
			if w.Code != http.StatusOK && w.Code != http.StatusCreated {
				t.Errorf("status = %d, want 200 or 201; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

// requiresFields reports whether the generated document marks any property of
// this route's request body as required. Reading it from the document rather
// than hardcoding a list is the point: if the tags and the document ever
// disagree, this test is what notices.
func requiresFields(t *testing.T, ginPath string) bool {
	t.Helper()
	doc := generatedDoc(t)
	path := strings.ReplaceAll(ginPath, ":id", "{id}")
	for _, op := range doc.Paths[path] {
		if op.RequestBody == nil {
			continue
		}
		media, ok := op.RequestBody.Content["application/json"]
		if !ok || media.Schema == nil {
			continue
		}
		name := strings.TrimPrefix(media.Schema.Ref, "#/components/schemas/")
		if schema := doc.Components.Schemas[name]; schema != nil && len(schema.Required) > 0 {
			return true
		}
	}
	return false
}

var (
	docOnce sync.Once
	docVal  *specter.Document
	docErr  error
)

// generatedDoc builds the document once for the whole test binary; scanning the
// package is not cheap enough to redo per subtest.
func generatedDoc(t *testing.T) *specter.Document {
	t.Helper()
	docOnce.Do(func() {
		docVal, docErr = specter.Generate(specter.Config{Dir: ".", Title: "shop", Version: "test"})
	})
	if docErr != nil {
		t.Fatalf("generate: %v", docErr)
	}
	return docVal
}

// A malformed body must come back as 400 with an error message, not a panic
// or a silent success.
func TestWritesRejectMalformedJSON(t *testing.T) {
	r := freshRouter(t)
	checked := 0
	for _, rt := range apiRoutes(r) {
		if rt.Method != http.MethodPost && rt.Method != http.MethodPut {
			continue
		}
		t.Run(rt.Method+" "+rt.Path, func(t *testing.T) {
			path := strings.ReplaceAll(rt.Path, ":id", "1")
			w := do(t, r, rt.Method, path, "{not json")
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("error response is not JSON: %v", err)
			}
			if body["error"] == nil {
				t.Errorf("body = %s, want an error field", w.Body.String())
			}
		})
		checked++
	}
	if checked == 0 {
		t.Fatal("no write routes found to check")
	}
}

// List endpoints read query parameters; passing them must not change the
// contract.
func TestListEndpointsAcceptQueryParams(t *testing.T) {
	r := freshRouter(t)
	for _, rt := range apiRoutes(r) {
		if rt.Method != http.MethodGet || strings.Contains(rt.Path, ":id") {
			continue
		}
		t.Run(rt.Path, func(t *testing.T) {
			w := do(t, r, http.MethodGet, rt.Path+"?q=x&status=active&limit=5&sort=name", "")
			// /docs redirects to /docs/ so the console's relative fetches
			// resolve; every other listing answers directly.
			if w.Code != http.StatusOK && w.Code != http.StatusMovedPermanently {
				t.Errorf("status = %d, want 200 (or 301 for /docs)", w.Code)
			}
		})
	}
}

// An unregistered path is a 404, which confirms the group prefix is applied.
func TestUnknownPathIs404(t *testing.T) {
	r := freshRouter(t)
	if w := do(t, r, http.MethodGet, "/api/v1/nope", ""); w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if w := do(t, r, http.MethodGet, "/users", ""); w.Code != http.StatusNotFound {
		t.Errorf("unprefixed path: status = %d, want 404", w.Code)
	}
}

func TestRouterRegistersAllResources(t *testing.T) {
	r := freshRouter(t)
	seen := map[string]bool{}
	for _, rt := range r.Routes() {
		seen[rt.Method+" "+rt.Path] = true
	}
	// Spot-check the shape the generated spec depends on.
	for _, want := range []string{
		"GET /api/v1/users",
		"GET /api/v1/users/:id",
		"POST /api/v1/users",
		"PUT /api/v1/users/:id",
		"DELETE /api/v1/users/:id",
	} {
		if !seen[want] {
			t.Errorf("route %q not registered", want)
		}
	}
}
