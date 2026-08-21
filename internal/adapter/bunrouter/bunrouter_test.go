package bunrouter

import (
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

	want := []string{
		"get /health",
		"get /api/v1/users",
		"get /api/v1/users/{id}",
		"post /api/v1/users",
		"delete /api/v1/users/{id}",
		"get /api/v1/files/{path}",
	}
	for _, key := range want {
		if _, ok := m[key]; !ok {
			t.Errorf("missing route %q; have %v", key, keys(m))
		}
	}
	if len(routes) != len(want) {
		t.Errorf("routes = %d, want %d: %v", len(routes), len(want), keys(m))
	}

	// Group prefix, handler name and doc comment all carry through.
	list := m["get /api/v1/users"]
	if list.HandlerName != "listUsers" {
		t.Errorf("handler = %q", list.HandlerName)
	}
	if list.Summary != "returns every user." {
		t.Errorf("summary = %q", list.Summary)
	}
	if schemas["User"] == nil {
		t.Errorf("User schema not collected: %v", schemas)
	}
}

func hasRoute(routes []core.Route, method, path string) bool {
	for _, r := range routes {
		if r.Method == method && r.Path == path {
			return true
		}
	}
	return false
}

func TestBunrouterLocalPrefix(t *testing.T) {
	routes, _, diags, err := (&Adapter{}).Scan("testdata/localprefix")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("diags = %v, want none", diags)
	}
	if !hasRoute(routes, "get", "/v1/categories") {
		t.Errorf("missing route get /v1/categories; have %v", routes)
	}
}

func TestName(t *testing.T) {
	if (&Adapter{}).Name() != "bunrouter" {
		t.Errorf("name = %q", (&Adapter{}).Name())
	}
}

func keys(m map[string]core.Route) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Middleware reaches routes three ways: the constructor option, a Use()
// statement scoped to the group it is in, and a chained receiver scoped to one
// route.
func TestBunrouterMiddleware(t *testing.T) {
	routes, _, _, err := (&Adapter{}).Scan("testdata/middleware")
	if err != nil {
		t.Fatal(err)
	}
	m := routeMap(routes)

	cases := []struct {
		key  string
		want []string
	}{
		{"get /health", []string{"RequestLogger"}},
		{"get /search", []string{"RequestLogger", "RateLimit"}},
		{"get /api/users", []string{"RequestLogger", "AuthRequired"}},
		{"post /api/users", []string{"RequestLogger", "AuthRequired"}},
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

	// A route-scoped middleware must not leak into what follows it.
	for _, mw := range m["get /api/users"].Middleware {
		if mw.Name == "RateLimit" {
			t.Errorf("RateLimit leaked out of get /search: %+v", m["get /api/users"].Middleware)
		}
	}
	for _, mw := range m["get /api/users"].Middleware {
		if mw.Name == "AuthRequired" && (mw.Kind != "auth" || mw.Scheme != "bearerAuth") {
			t.Errorf("AuthRequired = %+v, want auth/bearerAuth", mw)
		}
	}
}
