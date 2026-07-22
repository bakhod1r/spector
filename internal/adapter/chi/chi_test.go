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
