package echo

import (
	"sort"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// Name resolution, helper packages, response envelopes and group prefixes that
// cross a function boundary are properties of Go and of how services are
// written, not of a router. An echo project with the same layered shape as a
// gin one has to document the same way; before the resolution context was
// shared, echo read nothing but the handler's own body, matched every name
// against a flat table, and lost any prefix passed to a function.
func TestLayeredResolvesLikeAnyFramework(t *testing.T) {
	routes, schemas, _, err := (&Adapter{}).Scan("testdata/layered")
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]core.Route{}
	for _, r := range routes {
		byPath[r.Method+" "+r.Path] = r
	}

	create, ok := byPath["post /api/v1/users"]
	if !ok {
		t.Fatalf("route missing; have %v", routeKeys(byPath))
	}
	if create.RequestType != "CreateUserReq" {
		t.Errorf("request = %q, want CreateUserReq", create.RequestType)
	}
	if create.ResponseType != "EnvelopeOfUserResp" {
		t.Errorf("response = %q, want EnvelopeOfUserResp", create.ResponseType)
	}
	if s := schemas["EnvelopeOfUserResp"]; s == nil {
		t.Fatal("envelope schema missing")
	} else if got := s.Properties["data"].Ref; got != "#/components/schemas/UserResp" {
		t.Errorf("data.$ref = %q", got)
	}

	// Two contexts declare Create; a flat name table gave both the same body.
	orders, ok := byPath["post /api/v1/orders"]
	if !ok {
		t.Fatalf("orders route missing; have %v", routeKeys(byPath))
	}
	if orders.ResponseType != "EnvelopeOfOrderResp" {
		t.Errorf("orders response = %q, want EnvelopeOfOrderResp", orders.ResponseType)
	}
	if orders.RequestType != "" {
		t.Errorf("orders request = %q, want none", orders.RequestType)
	}
	if got := byPath["get /api/v1/orders"].ResponseType; got != "EnvelopeOfOrderRespList" {
		t.Errorf("orders list response = %q", got)
	}
}

func routeKeys(m map[string]core.Route) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
