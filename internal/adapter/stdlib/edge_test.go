package stdlib

import "testing"

// TestScanBrokenDir verifies a parse failure is reported, not swallowed.
func TestScanBrokenDir(t *testing.T) {
	if _, _, err := (&Adapter{}).Scan("testdata/broken"); err == nil {
		t.Fatal("Scan of an unparsable dir returned nil error")
	}
}

// TestScanEdge covers wrong arities, dynamic patterns, unknown methods,
// mounts without a nameable handler, ListenAndServe/TLS wrapping, and
// non-transparent http.* wrappers.
func TestScanEdge(t *testing.T) {
	routes, _, err := (&Adapter{}).Scan("testdata/edge")
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string][]string{}
	for _, r := range routes {
		var mws []string
		for _, mw := range r.Middleware {
			mws = append(mws, mw.Name)
		}
		byPath[r.Method+" "+r.Path] = mws
	}
	if _, ok := byPath["get /a"]; !ok {
		t.Errorf("route with a selector-wrapped handler missing: %v", byPath)
	}
	if _, ok := byPath["get /b"]; !ok {
		t.Errorf("route wrapped in a non-plumbing http call missing: %v", byPath)
	}
	if _, ok := byPath["trace /t"]; ok {
		t.Errorf("unknown method surfaced as a route: %v", byPath)
	}
	// logging(mux) handed to ListenAndServe wraps every route on the mux.
	found := false
	for _, mws := range byPath {
		for _, n := range mws {
			if n == "logging" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("served-handler wrapper not applied to any route: %v", byPath)
	}
}
