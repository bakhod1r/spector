package stdlib

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
	if create, ok := m["post /api/v1/users"]; !ok || create.RequestType != "CreateUserReq" || create.ResponseType != "User" {
		t.Errorf("create = %+v", create)
	}
}

// net/http has no Use: middleware is applied by wrapping, so the wrappers are
// the only place a guard shows up. A scan that ignored them would document
// every one of these endpoints as public.
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

	// Wrapping the server's handler applies to everything it serves.
	if !has("get /api/v1/users", "logging") {
		t.Errorf("server-level wrapper missing: %v", names("get /api/v1/users"))
	}
	if !has("get /health", "logging") {
		t.Errorf("server-level wrapper missing on /health: %v", names("get /health"))
	}
	// Wrapping a mounted sub-mux applies to every route on it, and to nothing
	// outside it.
	if !has("get /api/v1/users", "apiKeyGuard") {
		t.Errorf("mount wrapper missing: %v", names("get /api/v1/users"))
	}
	if has("get /health", "apiKeyGuard") {
		t.Errorf("mount wrapper leaked outside the sub-mux: %v", names("get /health"))
	}
	// Wrapping one handler applies to that route alone.
	if !has("delete /api/v1/users/{id}", "adminOnly") {
		t.Errorf("per-route wrapper missing: %v", names("delete /api/v1/users/{id}"))
	}
	if has("get /api/v1/users", "adminOnly") {
		t.Errorf("per-route wrapper leaked: %v", names("get /api/v1/users"))
	}

	// A wrapped handler is still the handler: its facts must survive.
	if del := m["delete /api/v1/users/{id}"]; del.HandlerName != "deleteUser" {
		t.Errorf("handler = %q, want deleteUser through the wrapper", del.HandlerName)
	}

	for _, mw := range m["get /api/v1/users"].Middleware {
		if mw.Name != "apiKeyGuard" {
			continue
		}
		if mw.Kind != "auth" {
			t.Errorf("kind = %q, want auth", mw.Kind)
		}
		if len(mw.Statuses) != 1 || mw.Statuses[0] != 401 {
			t.Errorf("statuses = %v, want [401]", mw.Statuses)
		}
	}
}

func TestSplitPattern(t *testing.T) {
	cases := map[string][2]string{
		"GET /users/{id}":   {"get", "/users/{id}"},
		"POST /users":       {"post", "/users"},
		"GET /files/{p...}": {"get", "/files/{p}"},
	}
	for pattern, want := range cases {
		m, p, ok := splitPattern(pattern)
		if !ok || m != want[0] || p != want[1] {
			t.Errorf("%q -> %q %q ok=%v, want %q %q", pattern, m, p, ok, want[0], want[1])
		}
	}
	if _, _, ok := splitPattern("/no-method"); ok {
		t.Error("expected methodless pattern to be skipped")
	}
}
