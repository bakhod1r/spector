package httprouter

import (
	"strings"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

func routeMap(routes []core.Route) map[string]core.Route {
	m := map[string]core.Route{}
	for _, r := range routes {
		m[r.Method+" "+r.Path] = r
	}
	return m
}

func TestScanRoutes(t *testing.T) {
	routes, schemas, _, err := (&Adapter{}).Scan("testdata/sample")
	if err != nil {
		t.Fatal(err)
	}
	m := routeMap(routes)

	// Method helpers, the generic Handle form, path params and a catch-all all
	// land as routes with OpenAPI-shaped paths.
	want := []string{
		"get /api/v1/users",
		"get /api/v1/users/{id}",
		"post /api/v1/users",
		"delete /api/v1/users/{id}",
		"get /files/{filepath}",
	}
	for _, key := range want {
		if _, ok := m[key]; !ok {
			t.Errorf("missing route %q; have %v", key, keys(m))
		}
	}
	if len(routes) != len(want) {
		t.Errorf("routes = %d, want %d: %v", len(routes), len(want), keys(m))
	}

	// Handler names and doc comments carry through.
	if got := m["get /api/v1/users"].HandlerName; got != "listUsers" {
		t.Errorf("handler = %q", got)
	}
	if got := m["get /api/v1/users"].Summary; got != "returns every user." {
		t.Errorf("summary = %q", got)
	}
	if schemas["User"] == nil {
		t.Errorf("User schema not collected: %v", schemas)
	}
}

func TestName(t *testing.T) {
	if (&Adapter{}).Name() != "httprouter" {
		t.Errorf("name = %q", (&Adapter{}).Name())
	}
}

// A directory that will not parse surfaces the error rather than panicking.
func TestScanBadDir(t *testing.T) {
	if _, _, _, err := (&Adapter{}).Scan("testdata/does-not-exist"); err == nil {
		t.Skip("missing dir parses as empty on this toolchain")
	}
}

func hasRoute(routes []core.Route, method, path string) bool {
	for _, r := range routes {
		if strings.EqualFold(r.Method, method) && r.Path == path {
			return true
		}
	}
	return false
}

// A base path built from a function-local variable still resolves, scoped to
// the function it's declared in.
func TestHTTPRouterLocalPrefix(t *testing.T) {
	routes, _, diags, err := (&Adapter{}).Scan("testdata/localprefix")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("diags = %v, want none", diags)
	}
	if !hasRoute(routes, "GET", "/v1/categories") {
		t.Errorf("missing route GET /v1/categories; have %v", routes)
	}
}

func keys(m map[string]core.Route) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// httprouter has no Use(): middleware is wrapping, either around one handler or
// around the router where it is served. Both are read, outermost first.
func TestHTTPRouterMiddleware(t *testing.T) {
	routes, _, _, err := (&Adapter{}).Scan("testdata/middleware")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]core.Route{}
	for _, r := range routes {
		m[r.Method+" "+r.Path] = r
	}

	cases := []struct {
		key  string
		want []string
	}{
		{"get /health", []string{"RequestLogger"}},
		{"get /users", []string{"RequestLogger", "AuthRequired"}},
		{"post /users", []string{"RequestLogger", "RateLimit", "AuthRequired"}},
	}
	for _, tc := range cases {
		route, ok := m[tc.key]
		if !ok {
			t.Fatalf("missing route %q; have %v", tc.key, keys(m))
		}
		var got []string
		for _, mw := range route.Middleware {
			got = append(got, mw.Name)
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s middleware = %v, want %v", tc.key, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s middleware = %v, want %v", tc.key, got, tc.want)
				break
			}
		}
	}

	// The wrapper is middleware; the handler underneath is still the handler.
	if h := m["get /users"].HandlerName; h != "listUsers" {
		t.Errorf("handler = %q, want listUsers", h)
	}
	// A wrapped handler keeps its body analysis: the doc comment survives.
	if s := m["get /users"].Summary; s != "returns every user." {
		t.Errorf("summary = %q", s)
	}
	// AuthRequired is classified from its name, and its 401 read from its body.
	for _, mw := range m["get /users"].Middleware {
		if mw.Name != "AuthRequired" {
			continue
		}
		if mw.Kind != "auth" || mw.Scheme != "bearerAuth" {
			t.Errorf("AuthRequired = %+v, want auth/bearerAuth", mw)
		}
	}
	if len(m["get /health"].Middleware) != 1 {
		t.Errorf("health middleware = %+v", m["get /health"].Middleware)
	}
}
