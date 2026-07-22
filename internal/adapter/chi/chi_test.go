package chi

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

// chi nests its groups in closures that shadow the router variable, so what is
// in scope has to be tracked as the walk descends rather than looked up by
// name. Getting this wrong leaks a guard onto sibling subtrees, and the
// document looks entirely plausible while being wrong.
func TestMiddlewareInferred(t *testing.T) {
	routes, _, err := (&Adapter{}).Scan("testdata/sample")
	if err != nil {
		t.Fatal(err)
	}
	m := routeMap(routes)

	names := func(key string) []string {
		var out []string
		for _, mw := range m[key].Middleware {
			out = append(out, mw.Name)
		}
		return out
	}
	has := func(key, want string) bool {
		for _, n := range names(key) {
			if n == want {
				return true
			}
		}
		return false
	}

	if !has("get /api/v1/users", "requestID") {
		t.Errorf("root Use not inherited: %v", names("get /api/v1/users"))
	}
	if !has("get /api/v1/users", "apiKeyGuard") {
		t.Errorf("Use inside Route not applied: %v", names("get /api/v1/users"))
	}
	if !has("delete /api/v1/users/{id}", "adminOnly") {
		t.Errorf("With() middleware missing: %v", names("delete /api/v1/users/{id}"))
	}
	if has("get /public/status", "apiKeyGuard") {
		t.Errorf("guard leaked to a sibling subtree: %v", names("get /public/status"))
	}
	if has("get /health", "apiKeyGuard") {
		t.Errorf("guard leaked to a route outside the group: %v", names("get /health"))
	}
	if !has("get /health", "requestID") {
		t.Errorf("root Use missing on /health: %v", names("get /health"))
	}

	// The body is the evidence: an unrecognised name that reads a key header
	// and answers 401 is a guard.
	for _, mw := range m["get /api/v1/users"].Middleware {
		if mw.Name != "apiKeyGuard" {
			continue
		}
		if mw.Kind != "auth" {
			t.Errorf("kind = %q, want auth", mw.Kind)
		}
		if len(mw.Statuses) != 1 || mw.Statuses[0] != 401 {
			t.Errorf("statuses = %v, want [401] from http.Error", mw.Statuses)
		}
	}
}

func TestScan(t *testing.T) {
	a := &Adapter{}
	routes, schemas, err := a.Scan("testdata/sample")
	if err != nil {
		t.Fatal(err)
	}
	if schemas["User"] == nil || schemas["CreateUserReq"] == nil {
		t.Fatalf("missing schemas: %v", schemas)
	}

	m := routeMap(routes)

	list, ok := m["get /api/v1/users"]
	if !ok || list.ResponseType != "User" || !list.ResponseArray {
		t.Errorf("list = %+v", list)
	}
	if len(list.QueryParams) != 1 || list.QueryParams[0] != "limit" {
		t.Errorf("list query = %v, want [limit]", list.QueryParams)
	}
	if len(list.HeaderParams) != 1 || list.HeaderParams[0] != "X-Tenant" {
		t.Errorf("list header = %v, want [X-Tenant]", list.HeaderParams)
	}
	if get, ok := m["get /api/v1/users/{id}"]; !ok || get.ResponseType != "User" || get.ResponseArray {
		t.Errorf("get = %+v", get)
	}
	create, ok := m["post /api/v1/users"]
	if !ok || create.RequestType != "CreateUserReq" || create.ResponseType != "User" {
		t.Errorf("create = %+v", create)
	}
}
