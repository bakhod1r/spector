package fiber

import (
	"testing"

	"github.com/bakhod1r/spector/internal/adapter/astutil"
	"github.com/bakhod1r/spector/internal/core"
)

func scanEdge(t *testing.T) map[string]core.Route {
	t.Helper()
	routes, _, _, err := (&Adapter{}).Scan("testdata/edge")
	if err != nil {
		t.Fatal(err)
	}
	return routeMap(routes)
}

func TestScanParseError(t *testing.T) {
	if _, _, _, err := (&Adapter{}).Scan("testdata/broken"); err == nil {
		t.Fatal("want parse error")
	}
}

func TestEdgeCases(t *testing.T) {
	rs := scanEdge(t)

	// Add with a selector method name (http.MethodPost).
	if r, ok := rs["post /added-sel"]; !ok || r.HandlerName != "addedSel" {
		t.Errorf("post /added-sel = %+v, %v", r, ok)
	}
	// Unknown method and variable-named method Add calls are skipped; the
	// method name itself isn't scope-resolved, only the path is.
	for _, k := range []string{"trace /nope", "get /var-method"} {
		if _, ok := rs[k]; ok {
			t.Errorf("%s registered", k)
		}
	}
	// A function-local string var used as a path resolves via the
	// scope-aware Resolver, so it's no longer a "dynamic" path.
	if _, ok := rs["get /dyn"]; !ok {
		t.Error("local-var path route missing")
	}
	// A group reassigned from itself — v1 = v1.Group("/again") — must not
	// loop, and must not retroactively move a route registered before the
	// reassignment: v1.Get("/nested") was written while v1 still meant
	// /api/v1, so that is where it belongs.
	if _, ok := rs["get /api/v1/nested"]; !ok {
		t.Errorf("nested group route missing; got %v", keys(rs))
	}
	// A group prefix built from a function-local var now resolves too, so
	// the route carries the full prefix rather than a bare path.
	if _, ok := rs["get /dyn/x"]; !ok {
		t.Error("local-var group prefix route missing")
	}
	if _, ok := rs["get /y"]; !ok {
		t.Error("call-receiver route missing")
	}
}

func TestFiberReportsDynamicRoute(t *testing.T) {
	_, _, diags, err := (&Adapter{}).Scan("testdata/edge")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 {
		t.Fatalf("diags = %d, want 1: %+v", len(diags), diags)
	}
	if diags[0].Kind != "route" {
		t.Errorf("kind = %q, want route", diags[0].Kind)
	}
	if diags[0].Pos.Line == 0 || diags[0].Pos.Filename == "" {
		t.Errorf("diagnostic has no source position: %+v", diags[0])
	}
}

func TestSplitHandlersEmpty(t *testing.T) {
	if h, inline := splitHandlers(nil); h != nil || inline != nil {
		t.Errorf("empty args: %v, %v", h, inline)
	}
}

func TestAddRouteNilHandler(t *testing.T) {
	var routes []core.Route
	addRoute("get", "/x", nil, nil, nil, nil, nil, nil, &routes, astutil.Locator{}, nil, nil, nil, nil)
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

// Group resolution — including the cycle guard this used to cover — now lives
// in astutil, shared by every adapter that spells a group as router.Group(...);
// see astutil.TestGroupResolverCycle.
