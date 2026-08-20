package gin

import (
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// Two contexts each declare their own Envelope. Looking the envelope's fields
// up by name across the whole tree finds two declarations and, refusing to pick
// one, resolves to neither: both endpoints then documented the bare envelope
// with an `any` payload. The handler's own package has to answer first.
func TestOwnEnvelopePerContextCarriesPayload(t *testing.T) {
	routes, schemas, _, err := (&Adapter{}).Scan("testdata/ownenvelope")
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]core.Route{}
	for _, r := range routes {
		byPath[r.Method+" "+r.Path] = r
	}
	for path, want := range map[string]string{
		"post /api/v1/users":  "EnvelopeOfUserResponse",
		"post /api/v1/orders": "EnvelopeOfOrderResponse",
	} {
		r, ok := byPath[path]
		if !ok {
			t.Errorf("route %q missing; got %v", path, paths(byPath))
			continue
		}
		if r.ResponseType != want {
			t.Errorf("%s response = %q, want %q", path, r.ResponseType, want)
			continue
		}
		s, ok := schemas[want]
		if !ok {
			t.Errorf("%s not synthesised; have %v", want, schemaNames(schemas))
			continue
		}
		payload := "UserResponse"
		if want == "EnvelopeOfOrderResponse" {
			payload = "OrderResponse"
		}
		data := s.Properties["data"]
		if data == nil {
			t.Errorf("%s has no data property: %+v", want, s.Properties)
			continue
		}
		if ref := "#/components/schemas/" + payload; data.Ref != ref {
			t.Errorf("%s data.$ref = %q, want %q", want, data.Ref, ref)
		}
	}
}
