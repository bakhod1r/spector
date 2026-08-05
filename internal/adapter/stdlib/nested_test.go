package stdlib

import "testing"

// Two levels of sub-mux mounting compose both prefixes onto the leaf routes,
// and the guard wrapping each level applies to every route beneath it.
func TestNestedSubMux(t *testing.T) {
	routes, _, _, err := (&Adapter{}).Scan("testdata/nested")
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]bool{}
	names := map[string][]string{}
	for _, r := range routes {
		byPath[r.Method+" "+r.Path] = true
		var mw []string
		for _, m := range r.Middleware {
			mw = append(mw, m.Name)
		}
		names[r.Method+" "+r.Path] = mw
	}

	// /api/v1 + /v2 + /items — both prefixes compose, not just the innermost.
	for _, want := range []string{"get /api/v1/v2/items", "get /api/v1/v2/items/{id}"} {
		if !byPath[want] {
			t.Errorf("missing route %q; have %v", want, byPath)
		}
	}

	// The root requestID and the /api/v1 apiKeyGuard both reach the leaf.
	mw := names["get /api/v1/v2/items"]
	has := func(n string) bool {
		for _, m := range mw {
			if m == n {
				return true
			}
		}
		return false
	}
	if !has("requestID") {
		t.Errorf("root requestID not applied through the chain: %v", mw)
	}
	if !has("apiKeyGuard") {
		t.Errorf("mount guard not applied through the chain: %v", mw)
	}
}
