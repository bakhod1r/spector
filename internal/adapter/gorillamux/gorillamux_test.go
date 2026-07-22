package gorillamux

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

func scan(t *testing.T) (map[string]core.Route, map[string]*core.Schema) {
	t.Helper()
	routes, schemas, err := (&Adapter{}).Scan("testdata/sample")
	if err != nil {
		t.Fatal(err)
	}
	return routeMap(routes), schemas
}

func TestName(t *testing.T) {
	if got := (&Adapter{}).Name(); got != "gorillamux" {
		t.Errorf("Name = %q, want gorillamux", got)
	}
}

func TestScanCollectsSchemas(t *testing.T) {
	_, schemas := scan(t)
	for _, want := range []string{"User", "CreateUserReq"} {
		if schemas[want] == nil {
			t.Errorf("schema %q missing; got %v", want, keys(schemas))
		}
	}
}

// Subrouters nest, so a route on the inner one carries both prefixes.
func TestNestedSubrouterPrefixes(t *testing.T) {
	m, _ := scan(t)
	for _, want := range []string{
		"get /api/v1/users",
		"get /api/v1/users/{id}",
		"post /api/v1/users",
		"delete /api/v1/users/{id}",
	} {
		if _, ok := m[want]; !ok {
			t.Errorf("route %q missing; got %v", want, keys(m))
		}
	}
}

// {id:[0-9]+} carries a regex the document does not want.
func TestRegexStrippedFromParams(t *testing.T) {
	m, _ := scan(t)
	if _, ok := m["get /api/v1/users/{id}"]; !ok {
		t.Errorf("{id:[0-9]+} was not rewritten to {id}; got %v", keys(m))
	}
}

// A methodless HandleFunc still appears, as a get.
func TestMethodlessRouteDefaultsToGet(t *testing.T) {
	m, _ := scan(t)
	if _, ok := m["get /health"]; !ok {
		t.Errorf("methodless route missing; got %v", keys(m))
	}
	// It must not also be double-reported by the chain pass.
	count := 0
	for k := range m {
		if k == "get /health" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("route reported %d times", count)
	}
}

func TestMethodsRegistersEachListed(t *testing.T) {
	m, _ := scan(t)
	if _, ok := m["get /dual"]; !ok {
		t.Errorf("Methods did not register get; got %v", keys(m))
	}
	if _, ok := m["post /dual"]; !ok {
		t.Errorf("Methods did not register post; got %v", keys(m))
	}
	if _, ok := m["delete /dual"]; ok {
		t.Error("Methods registered a method that was not listed")
	}
}

// http.MethodPost is a selector, not a literal, and still counts.
func TestMethodConstantRecognised(t *testing.T) {
	m, _ := scan(t)
	if _, ok := m["post /api/v1/users"]; !ok {
		t.Errorf("http.MethodPost not recognised; got %v", keys(m))
	}
}

// Handlers are plain net/http, whose spellings astutil already knows.
func TestHandlerFactsExtracted(t *testing.T) {
	m, _ := scan(t)

	list := m["get /api/v1/users"]
	if list.ResponseType != "User" || !list.ResponseArray {
		t.Errorf("list response = %q/%v, want a User array", list.ResponseType, list.ResponseArray)
	}
	if len(list.QueryParams) != 2 {
		t.Errorf("query params = %v, want q and limit", list.QueryParams)
	}

	create := m["post /api/v1/users"]
	if create.RequestType != "CreateUserReq" {
		t.Errorf("request = %q, want CreateUserReq", create.RequestType)
	}

	del := m["delete /api/v1/users/{id}"]
	if len(del.Responses) == 0 || del.Responses[0].Status != 204 {
		t.Errorf("responses = %+v, want a 204 from WriteHeader", del.Responses)
	}

	health := m["get /health"]
	if len(health.HeaderParams) != 1 || health.HeaderParams[0] != "X-Token" {
		t.Errorf("header params = %v, want [X-Token]", health.HeaderParams)
	}
}

func TestDocCommentBecomesSummary(t *testing.T) {
	m, _ := scan(t)
	list := m["get /api/v1/users"]
	if list.Summary != "returns every user." {
		t.Errorf("summary = %q", list.Summary)
	}
}

// r.Use on the router and on the subrouter both reach the route.
func TestMiddlewareInferred(t *testing.T) {
	m, _ := scan(t)

	names := func(r core.Route) []string {
		var out []string
		for _, mw := range r.Middleware {
			out = append(out, mw.Name)
		}
		return out
	}
	has := func(r core.Route, want string) bool {
		for _, n := range names(r) {
			if n == want {
				return true
			}
		}
		return false
	}

	list := m["get /api/v1/users"]
	if !has(list, "tenantGuard") {
		t.Errorf("subrouter middleware missing: %v", names(list))
	}

	if health := m["get /health"]; has(health, "tenantGuard") {
		t.Errorf("subrouter middleware leaked onto /health: %v", names(health))
	}
}

func TestScanMissingDirErrors(t *testing.T) {
	if _, _, err := (&Adapter{}).Scan("testdata/does-not-exist"); err == nil {
		t.Error("expected an error for a missing dir")
	}
}

func TestScanEmptyDir(t *testing.T) {
	routes, _, err := (&Adapter{}).Scan(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 {
		t.Errorf("routes = %v, want none", routes)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
