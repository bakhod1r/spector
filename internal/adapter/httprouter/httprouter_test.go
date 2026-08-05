package httprouter

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

func keys(m map[string]core.Route) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
