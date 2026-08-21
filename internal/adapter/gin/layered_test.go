package gin

import (
	"sort"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// scanLayered documents the shape a real service has once it grows past one
// package: bounded contexts that each declare Mount and Create, routes
// registered through append(guards, h.X)..., a handler factory, an envelope
// helper every response goes through, and a group prefix that reaches the
// registration through three hops.
func scanLayered(t *testing.T) (map[string]core.Route, map[string]*core.Schema) {
	t.Helper()
	routes, schemas, _, err := (&Adapter{}).Scan("testdata/layered")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]core.Route{}
	for _, r := range routes {
		out[r.Method+" "+r.Path] = r
	}
	return out, schemas
}

func paths(routes map[string]core.Route) []string {
	var keys []string
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// A group that reaches a registration through a parameter, a bare sub-group and
// another package's method used to lose its prefix entirely, so every route in
// the context was documented at the root.
func TestLayeredGroupPrefixSurvivesEveryHop(t *testing.T) {
	routes, _ := scanLayered(t)
	for _, want := range []string{
		"post /api/v1/users",
		"get /api/v1/users/{userID}",
		"delete /api/v1/users/{userID}",
		"post /api/v1/users/{userID}/verify/email",
		"get /api/v1/orders",
		"post /api/v1/orders",
	} {
		if _, ok := routes[want]; !ok {
			t.Errorf("missing %q; got %v", want, paths(routes))
		}
	}
}

// The handler is the last element of an append, not the last argument, so the
// registration names no identifier at all. Reading the argument directly found
// no declaration and documented the endpoint with nothing.
func TestLayeredHandlerInsideAppend(t *testing.T) {
	routes, _ := scanLayered(t)
	r, ok := routes["post /api/v1/users"]
	if !ok {
		t.Fatalf("route not found: %v", paths(routes))
	}
	if r.HandlerName != "Create" {
		t.Errorf("handler = %q, want Create", r.HandlerName)
	}
	if r.RequestType != "CreateUserRequest" {
		t.Errorf("request = %q, want CreateUserRequest", r.RequestType)
	}
}

// Two contexts declare Create. Resolving by bare name gave both routes the
// same handler, so one context was documented from the other's body.
func TestLayeredCollidingNamesResolvePerPackage(t *testing.T) {
	routes, _ := scanLayered(t)
	users, ok := routes["post /api/v1/users"]
	if !ok {
		t.Fatal("users route missing")
	}
	orders, ok := routes["post /api/v1/orders"]
	if !ok {
		t.Fatal("orders route missing")
	}
	if users.ResponseType == orders.ResponseType {
		t.Fatalf("both Create routes resolved to %q", users.ResponseType)
	}
	if want := "EnvelopeOfUserResponse"; users.ResponseType != want {
		t.Errorf("users response = %q, want %q", users.ResponseType, want)
	}
	if want := "EnvelopeOfOrderResponse"; orders.ResponseType != want {
		t.Errorf("orders response = %q, want %q", orders.ResponseType, want)
	}
	// The orders context binds no body; the users one does. A shared
	// declaration would have given them the same request type.
	if orders.RequestType != "" {
		t.Errorf("orders request = %q, want none", orders.RequestType)
	}
}

// A handler factory's own body serves no request: the returned literal does.
func TestLayeredHandlerFactory(t *testing.T) {
	routes, _ := scanLayered(t)
	r, ok := routes["post /api/v1/users/{userID}/verify/email"]
	if !ok {
		t.Fatalf("route not found: %v", paths(routes))
	}
	if r.HandlerName != "Verify" {
		t.Errorf("handler = %q, want Verify", r.HandlerName)
	}
	if r.RequestType != "CreateUserRequest" {
		t.Errorf("request = %q, want CreateUserRequest", r.RequestType)
	}
	if r.ResponseType != "EnvelopeOfUserResponse" {
		t.Errorf("response = %q, want EnvelopeOfUserResponse", r.ResponseType)
	}
}

// Every response goes through Envelope{Data: data}. Documenting the envelope
// alone names the same shape for every endpoint and no payload for any of
// them; the pairing has to become a schema of its own.
func TestLayeredEnvelopeCarriesPayload(t *testing.T) {
	routes, schemas := scanLayered(t)
	s, ok := schemas["EnvelopeOfUserResponse"]
	if !ok {
		t.Fatalf("envelope schema not synthesised; have %v", schemaNames(schemas))
	}
	data, ok := s.Properties["data"]
	if !ok {
		t.Fatalf("payload property missing: %+v", s.Properties)
	}
	if want := "#/components/schemas/UserResponse"; data.Ref != want {
		t.Errorf("data.$ref = %q, want %q", data.Ref, want)
	}
	// The envelope's own fields are still the endpoint's real response.
	if s.Properties["meta"] == nil || s.Properties["error"] == nil {
		t.Errorf("envelope fields lost: %+v", s.Properties)
	}
	// A list payload is a distinct schema, not the same one.
	list, ok := schemas["EnvelopeOfOrderResponseList"]
	if !ok {
		t.Fatalf("list envelope not synthesised; have %v", schemaNames(schemas))
	}
	if list.Properties["data"].Type != "array" {
		t.Errorf("list data = %+v, want array", list.Properties["data"])
	}
	if got := routes["get /api/v1/orders"].ResponseType; got != "EnvelopeOfOrderResponseList" {
		t.Errorf("orders list response = %q", got)
	}
}

// The 204 is written three calls out of the handler. At the old depth it was
// past the horizon and the operation carried no response at all.
func TestLayeredStatusThreeCallsDeep(t *testing.T) {
	routes, _ := scanLayered(t)
	r, ok := routes["delete /api/v1/users/{userID}"]
	if !ok {
		t.Fatalf("route not found: %v", paths(routes))
	}
	var found bool
	for _, resp := range r.Responses {
		if resp.Status == 204 {
			found = true
		}
	}
	if !found {
		t.Errorf("204 not documented: %+v", r.Responses)
	}
}

func schemaNames(schemas map[string]*core.Schema) []string {
	var names []string
	for k := range schemas {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
