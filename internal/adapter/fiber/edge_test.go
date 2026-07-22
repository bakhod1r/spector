package fiber

import (
	"testing"

	"github.com/user/specter/internal/adapter/astutil"
	"github.com/user/specter/internal/core"
)

func scanEdge(t *testing.T) map[string]core.Route {
	t.Helper()
	routes, _, err := (&Adapter{}).Scan("testdata/edge")
	if err != nil {
		t.Fatal(err)
	}
	return routeMap(routes)
}

func TestScanParseError(t *testing.T) {
	if _, _, err := (&Adapter{}).Scan("testdata/broken"); err == nil {
		t.Fatal("want parse error")
	}
}

func TestEdgeCases(t *testing.T) {
	rs := scanEdge(t)

	// Add with a selector method name (http.MethodPost).
	if r, ok := rs["post /added-sel"]; !ok || r.HandlerName != "addedSel" {
		t.Errorf("post /added-sel = %+v, %v", r, ok)
	}
	// Unknown, variable-named and dynamic-path Add calls are skipped.
	for _, k := range []string{"trace /nope", "get /var-method", "get /dyn"} {
		if _, ok := rs[k]; ok {
			t.Errorf("%s registered", k)
		}
	}
	// The self-assigned group keeps its last prefix without looping.
	if _, ok := rs["get /again/nested"]; !ok {
		t.Errorf("nested group route missing; got %v", keys(rs))
	}
	// Non-literal group prefix and call-receiver registrations keep bare paths.
	if _, ok := rs["get /x"]; !ok {
		t.Error("dynamic-prefix group route missing")
	}
	if _, ok := rs["get /y"]; !ok {
		t.Error("call-receiver route missing")
	}
}

func TestSplitHandlersEmpty(t *testing.T) {
	if h, inline := splitHandlers(nil); h != nil || inline != nil {
		t.Errorf("empty args: %v, %v", h, inline)
	}
}

func TestAddRouteNilHandler(t *testing.T) {
	var routes []core.Route
	addRoute("get", "/x", nil, nil, nil, nil, nil, nil, &routes, astutil.Locator{}, nil, nil)
	if len(routes) != 0 {
		t.Errorf("nil handler registered: %+v", routes)
	}
}

func TestCapitalize(t *testing.T) {
	cases := map[string]string{"": "", "get": "Get", "GET": "Get", "pATCH": "Patch"}
	for in, want := range cases {
		if got := capitalize(in); got != want {
			t.Errorf("capitalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveGroupCycle(t *testing.T) {
	groups := map[string]groupDef{
		"a": {recv: "b", prefix: "/a"},
		"b": {recv: "a", prefix: "/b"},
	}
	if got := resolveGroup("a", groups); got != "/b/a" {
		t.Errorf("cycle: got %q", got)
	}
	if got := resolveGroup("missing", groups); got != "" {
		t.Errorf("missing: got %q", got)
	}
}
