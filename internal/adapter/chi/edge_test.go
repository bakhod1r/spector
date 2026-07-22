package chi

import "testing"

// TestScanBrokenDir verifies a parse failure is reported, not swallowed.
func TestScanBrokenDir(t *testing.T) {
	if _, _, err := (&Adapter{}).Scan("testdata/broken"); err == nil {
		t.Fatal("Scan of an unparsable dir returned nil error")
	}
}

// TestScanEdge walks a fixture full of shapes the walker must skip gracefully:
// non-selector calls, dynamic paths, Route/Group with non-literal bodies,
// Mount, and a routing call whose receiver is a call other than With.
func TestScanEdge(t *testing.T) {
	routes, _, err := (&Adapter{}).Scan("testdata/edge")
	if err != nil {
		t.Fatal(err)
	}
	m := routeMap(routes)
	if _, ok := m["get /g"]; !ok {
		t.Errorf("route inside Group closure missing: %v", m)
	}
	if _, ok := m["get /w"]; !ok {
		t.Errorf("route on a call receiver missing: %v", m)
	}
	if _, ok := m["get /with"]; !ok {
		t.Errorf("With() route missing: %v", m)
	}
	// Dynamic-path registrations must not surface as routes.
	for k := range m {
		if k == "get pathVar" {
			t.Errorf("dynamic path surfaced as a route: %v", m)
		}
	}
}
