package gen

import (
	"strings"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// OpenAPI requires operationId to be unique across the document, and every
// client generator relies on it. Naming an operation after its handler gives
// two endpoints the same id as soon as two handler types spell a method the
// same way, which is the normal shape of a Go service.
func TestOperationIDsAreUnique(t *testing.T) {
	routes := []core.Route{
		{Method: "get", Path: "/v1/products/{productID}", HandlerName: "Get"},
		{Method: "get", Path: "/v1/orders/{orderID}", HandlerName: "Get"},
		{Method: "get", Path: "/v1/products", HandlerName: "List"},
	}
	doc := Build("t", "1", routes, map[string]*core.Schema{})

	seen := map[string]string{}
	for path, ops := range doc.Paths {
		for method, op := range ops {
			where := method + " " + path
			if op.OperationID == "" {
				t.Errorf("%s has no operationId", where)
				continue
			}
			if prev, dup := seen[op.OperationID]; dup {
				t.Errorf("operationId %q used by both %s and %s", op.OperationID, prev, where)
			}
			seen[op.OperationID] = where
		}
	}

	// A name nothing contests is still the handler's, which is the readable one.
	if got := doc.Paths["/v1/products"]["get"].OperationID; got != "List" {
		t.Errorf("uncontested operationId = %q, want List", got)
	}
}

// A contested name is qualified by the type the handler hangs off, because that
// is the fact separating the two handlers. Falling straight to the method-and-
// path form would be unique but would read like a URL in every generated
// client: post_v1_orders where orderCreate was available.
func TestContestedIDUsesReceiverType(t *testing.T) {
	routes := []core.Route{
		{Method: "get", Path: "/v1/products/{id}", HandlerName: "Get", HandlerType: "ProductHandler"},
		{Method: "get", Path: "/v1/orders/{id}", HandlerName: "Get", HandlerType: "*OrderController"},
	}
	doc := Build("t", "1", routes, map[string]*core.Schema{})

	if got := doc.Paths["/v1/products/{id}"]["get"].OperationID; got != "productGet" {
		t.Errorf("operationId = %q, want productGet", got)
	}
	if got := doc.Paths["/v1/orders/{id}"]["get"].OperationID; got != "orderGet" {
		t.Errorf("operationId = %q, want orderGet", got)
	}
}

// Two routes on one type share the receiver too, so qualifying by it settles
// nothing and the path form has to decide.
func TestContestedIDFallsBackWhenTypeDoesNotSeparate(t *testing.T) {
	routes := []core.Route{
		{Method: "get", Path: "/a", HandlerName: "Get", HandlerType: "Handler"},
		{Method: "get", Path: "/b", HandlerName: "Get", HandlerType: "Handler"},
	}
	doc := Build("t", "1", routes, map[string]*core.Schema{})

	a := doc.Paths["/a"]["get"].OperationID
	b := doc.Paths["/b"]["get"].OperationID
	if a == b {
		t.Fatalf("both operations are %q", a)
	}
	for path, id := range map[string]string{"/a": a, "/b": b} {
		if !strings.Contains(id, strings.Trim(path, "/")) {
			t.Errorf("%s: operationId = %q, want the path form", path, id)
		}
	}
}

// An id the author declared is a promise to their callers, so a collision
// elsewhere must not rewrite it.
func TestDeclaredOperationIDKept(t *testing.T) {
	routes := []core.Route{
		{Method: "get", Path: "/a", HandlerName: "Get", OperationID: "getA"},
		{Method: "get", Path: "/b", HandlerName: "Get"},
	}
	doc := Build("t", "1", routes, map[string]*core.Schema{})
	if got := doc.Paths["/a"]["get"].OperationID; got != "getA" {
		t.Errorf("declared operationId = %q, want getA", got)
	}
	if got := doc.Paths["/b"]["get"].OperationID; got == "getA" {
		t.Errorf("/b took the declared id %q", got)
	}
}
