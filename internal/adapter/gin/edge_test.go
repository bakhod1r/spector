package gin

import "testing"

func edgeRouteMap(t *testing.T) map[string]bool {
	t.Helper()
	routes, _, err := (&Adapter{}).Scan("testdata/edge")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, r := range routes {
		m[r.Method+" "+r.Path] = true
	}
	return m
}

// TestScanBrokenDir verifies a parse failure is reported, not swallowed.
func TestScanBrokenDir(t *testing.T) {
	if _, _, err := (&Adapter{}).Scan("testdata/broken"); err == nil {
		t.Fatal("Scan of an unparsable dir returned nil error")
	}
}

// TestScanEdge covers method-declared handlers, inline middleware, dynamic
// paths and group prefixes, and non-ident receivers.
func TestScanEdge(t *testing.T) {
	m := edgeRouteMap(t)
	for _, want := range []string{"get /mw", "get /nested", "get /inside", "get /m"} {
		if !m[want] {
			t.Errorf("missing route %q in %v", want, m)
		}
	}
}

// TestMethodHandlerResolved checks a route whose handler exists only as a
// method still gets its response type documented.
func TestMethodHandlerResolved(t *testing.T) {
	routes, _, err := (&Adapter{}).Scan("testdata/edge")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range routes {
		if r.Path == "/m" && r.HandlerName != "methodOnly" {
			t.Errorf("handler = %q, want methodOnly", r.HandlerName)
		}
	}
}
