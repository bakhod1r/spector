package gin

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
	if !ok {
		t.Fatal("missing GET /api/v1/users")
	}
	if list.ResponseType != "User" || !list.ResponseArray {
		t.Errorf("list response = %q array=%v, want User array", list.ResponseType, list.ResponseArray)
	}
	if len(list.QueryParams) != 1 || list.QueryParams[0] != "limit" {
		t.Errorf("list query = %v, want [limit]", list.QueryParams)
	}
	if len(list.HeaderParams) != 1 || list.HeaderParams[0] != "X-Tenant" {
		t.Errorf("list header = %v, want [X-Tenant]", list.HeaderParams)
	}

	get, ok := m["get /api/v1/users/{id}"]
	if !ok {
		t.Fatal("missing GET /api/v1/users/{id}")
	}
	if get.ResponseType != "User" || get.ResponseArray {
		t.Errorf("get response = %q array=%v, want User non-array", get.ResponseType, get.ResponseArray)
	}

	create, ok := m["post /api/v1/users"]
	if !ok {
		t.Fatal("missing POST /api/v1/users")
	}
	if create.RequestType != "CreateUserReq" {
		t.Errorf("create request = %q, want CreateUserReq", create.RequestType)
	}
	if create.ResponseType != "User" {
		t.Errorf("create response = %q, want User", create.ResponseType)
	}
}
