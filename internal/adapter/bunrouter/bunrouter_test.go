package bunrouter

import (
	"testing"

	"github.com/user/specter/internal/core"
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
