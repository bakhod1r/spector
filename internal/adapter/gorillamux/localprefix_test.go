package gorillamux

import "testing"

// A function-local variable, single-assigned to a string literal, used to
// build a route path — the Resolver must resolve it even though it is not a
// package-level const/var.
func TestGorillamuxLocalPrefix(t *testing.T) {
	routes, _, diags, err := (&Adapter{}).Scan("testdata/localprefix")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("diagnostics = %v, want none", diags)
	}
	if !hasRoute(routes, "get", "/v1/categories") {
		t.Errorf("route /v1/categories missing; got %v", routes)
	}
}
