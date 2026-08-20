package httprouter

import (
	"sort"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// Every adapter is tested against the same layered shape: two bounded contexts
// declaring the same method names, a generic helper package the handlers reach
// the framework through, and one response envelope. None of that is a property
// of the router, so none of it may document differently per framework.
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
		t.Fatalf("route missing; have %v", layeredKeys(byPath))
	}
	// Read through the helper package, two hops out of the handler.
	if create.RequestType != "CreateUserReq" {
		t.Errorf("request = %q, want CreateUserReq", create.RequestType)
	}
	// The envelope names the payload rather than being the payload.
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
		t.Fatalf("orders route missing; have %v", layeredKeys(byPath))
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

func layeredKeys(m map[string]core.Route) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
