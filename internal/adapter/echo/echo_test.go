package echo

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
	if got := (&Adapter{}).Name(); got != "echo" {
		t.Errorf("Name = %q, want echo", got)
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

// Groups nest, so a route on the inner group carries both prefixes.
func TestNestedGroupPrefixes(t *testing.T) {
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

// A route registered straight on the engine has no prefix.
func TestUngroupedRoute(t *testing.T) {
	m, _ := scan(t)
	if _, ok := m["get /health"]; !ok {
		t.Errorf("route missing; got %v", keys(m))
	}
}

func TestPathParamsNormalized(t *testing.T) {
	m, _ := scan(t)
	if _, ok := m["get /api/v1/users/{id}"]; !ok {
		t.Errorf(":id was not rewritten to {id}; got %v", keys(m))
	}
	// A trailing `*` is a wildcard, not a named parameter.
	if _, ok := m["get /files/{wildcard}"]; !ok {
		t.Errorf("wildcard not normalized; got %v", keys(m))
	}
}

// Any registers every method, so the document must not show only one.
func TestAnyExpandsToMethods(t *testing.T) {
	m, _ := scan(t)
	for _, method := range []string{"get", "post", "put", "patch", "delete"} {
		if _, ok := m[method+" /proxy"]; !ok {
			t.Errorf("Any did not register %s; got %v", method, keys(m))
		}
	}
}

func TestMatchRegistersListedMethods(t *testing.T) {
	m, _ := scan(t)
	if _, ok := m["get /dual"]; !ok {
		t.Errorf("Match did not register get; got %v", keys(m))
	}
	if _, ok := m["post /dual"]; !ok {
		t.Errorf("Match did not register post; got %v", keys(m))
	}
	if _, ok := m["delete /dual"]; ok {
		t.Error("Match registered a method that was not listed")
	}
}

// astutil recognises echo's own spellings: Bind, QueryParam, NoContent.
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
		t.Errorf("request = %q, want CreateUserReq (echo spells it Bind)", create.RequestType)
	}
	if create.ResponseType != "User" {
		t.Errorf("response = %q, want User", create.ResponseType)
	}

	del := m["delete /api/v1/users/{id}"]
	if len(del.Responses) == 0 || del.Responses[0].Status != 204 {
		t.Errorf("responses = %+v, want a 204 from NoContent", del.Responses)
	}

	health := m["get /health"]
	if len(health.HeaderParams) != 1 || health.HeaderParams[0] != "X-Token" {
		t.Errorf("header params = %v, want [X-Token]", health.HeaderParams)
	}
}

// Both status codes a handler can emit are documented, not just the first.
func TestMultipleResponsesRecorded(t *testing.T) {
	m, _ := scan(t)
	create := m["post /api/v1/users"]
	statuses := map[int]bool{}
	for _, r := range create.Responses {
		statuses[r.Status] = true
	}
	if !statuses[201] || !statuses[400] {
		t.Errorf("responses = %+v, want 201 and 400", create.Responses)
	}
}

func TestDocCommentBecomesSummary(t *testing.T) {
	m, _ := scan(t)
	list := m["get /api/v1/users"]
	if list.Summary != "returns every user." {
		t.Errorf("summary = %q", list.Summary)
	}
	if list.Description == "" {
		t.Errorf("description is empty")
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
