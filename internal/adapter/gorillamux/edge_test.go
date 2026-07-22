package gorillamux

import (
	"strings"
	"testing"
)

// The edge fixture exercises constructs the scanner must skip without
// crashing: variable paths, Methods chains on identifiers, subrouters
// assigned into map indexes, and PathPrefix arguments that are not literals.
func TestEdgeFixtureScans(t *testing.T) {
	routes, _, err := (&Adapter{}).Scan("testdata/edge")
	if err != nil {
		t.Fatal(err)
	}
	m := routeMap(routes)

	// A variable path cannot be documented, so neither registration appears.
	for k := range m {
		if k == "get /dynamic" {
			t.Errorf("variable path was documented: %v", keys(m))
		}
	}

	// route.Methods("GET") on an identifier is not a chain the scanner can
	// unpick, but the plain HandleFunc registration still appears as get.
	if _, ok := m["get /split"]; !ok {
		t.Errorf("split registration missing; got %v", keys(m))
	}

	// Methods(m) with a variable argument yields no declared methods, so the
	// route defaults to get.
	if _, ok := m["get /vararg"]; !ok {
		t.Errorf("vararg route missing; got %v", keys(m))
	}

	// Subrouters the collector cannot resolve contribute no prefix.
	for _, want := range []string{"get /plain", "get /hdr", "get /vp"} {
		if _, ok := m[want]; !ok {
			t.Errorf("route %q missing; got %v", want, keys(m))
		}
	}

	// The mapped subrouter must not have produced a phantom prefix anywhere.
	for k := range m {
		if strings.Contains(k, "/mapped") {
			t.Errorf("mapped subrouter leaked a prefix: %v", keys(m))
		}
	}
}
