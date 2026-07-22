package echo

import "testing"

// TestScanEdge covers Match with selector method constants, non-literal
// method lists, dynamic paths and group prefixes, and bare-star wildcards.
func TestScanEdge(t *testing.T) {
	routes, _, err := (&Adapter{}).Scan("testdata/edge")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, r := range routes {
		m[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"get /m2",              // literal "GET" inside a Match slice
		"get /m3", "post /m3",  // http.MethodGet / http.MethodPost selectors
		"get /inside",          // group with a dynamic prefix: no prefix applied
		"get /files/{wildcard}", // bare * segment
	} {
		if !m[want] {
			t.Errorf("missing route %q in %v", want, m)
		}
	}
	if m["get /mv"] {
		t.Errorf("Match with a non-literal method slice produced routes: %v", m)
	}
}
