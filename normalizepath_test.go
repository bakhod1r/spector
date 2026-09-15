package spector

import "testing"

// A group registered as r.Route("/products", …) + r.Get("/", …) reads out of
// the AST as "/products/", and the frameworks serve it at "/products". The
// document kept the slash while the Postman export dropped it, so one scan
// described a single endpoint at two URLs. Normalising once, where every
// adapter passes through, is what keeps the exports agreeing.
func TestNormalizePath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"/v1/products/", "/v1/products"},
		{"/v1/products/{productID}/", "/v1/products/{productID}"},
		{"/v1/products", "/v1/products"},
		{"/", "/"},
		{"", "/"},
		{"//", "/"},
	} {
		if got := normalizePath(tc.in); got != tc.want {
			t.Errorf("normalizePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
