package chi

import (
	"sort"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// Handler resolution, helper packages and response envelopes are properties of
// Go and of how services are written, not of a router. A chi project with the
// same layered shape as a gin one has to document the same way; before the
// resolution context was shared, chi read nothing but the handler's own body
// and matched every name against a flat table.
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
		t.Fatalf("route missing; have %v", keys(byPath))
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
		t.Fatalf("envelope schema missing; have %v", schemaNames(schemas))
	} else if got := s.Properties["data"].Ref; got != "#/components/schemas/UserResp" {
		t.Errorf("data.$ref = %q", got)
	}

	// Two contexts declare Create; a flat name table gave both the same body.
	orders, ok := byPath["post /api/v1/orders"]
	if !ok {
		t.Fatalf("orders route missing; have %v", keys(byPath))
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

func keys(m map[string]core.Route) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func schemaNames(m map[string]*core.Schema) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
