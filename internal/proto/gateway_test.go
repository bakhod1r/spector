package proto

import (
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

func gatewayDoc(t *testing.T) *core.Document {
	t.Helper()
	doc, err := ScanGateway("testdata_gateway")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func op(t *testing.T, doc *core.Document, path, method string) *core.Operation {
	t.Helper()
	o := doc.Paths[path][method]
	if o == nil {
		t.Fatalf("no %s %s; paths = %v", method, path, doc.Paths)
	}
	return o
}

func TestGatewayGetWithPathParam(t *testing.T) {
	doc := gatewayDoc(t)
	o := op(t, doc, "/v1/users/{user_id}", "get")
	if o.OperationID != "UserService_GetUser" {
		t.Errorf("operationId = %q", o.OperationID)
	}
	if len(o.Parameters) != 1 {
		t.Fatalf("parameters = %+v, want one path param", o.Parameters)
	}
	p := o.Parameters[0]
	if p.Name != "user_id" || p.In != "path" || !p.Required {
		t.Errorf("param = %+v, want required path user_id", p)
	}
	if o.RequestBody != nil {
		t.Error("a GET binding must not carry a request body")
	}
	if o.Responses["200"] == nil {
		t.Fatalf("responses = %v", o.Responses)
	}
	if ref := o.Responses["200"].Content["application/json"].Schema.Ref; ref != "#/components/schemas/shop.v1.User" {
		t.Errorf("response schema ref = %q", ref)
	}
	if doc.Components.Schemas["shop.v1.User"] == nil {
		t.Error("output message not registered in components")
	}
}

// Request fields that are not in the path become query parameters on a
// bodyless binding — that is how the gateway itself reads them.
func TestGatewayQueryParamsFromRequestFields(t *testing.T) {
	doc := gatewayDoc(t)
	o := op(t, doc, "/v1/users", "get")
	got := map[string]string{}
	for _, p := range o.Parameters {
		got[p.Name] = p.In
	}
	if got["limit"] != "query" || got["query"] != "query" {
		t.Errorf("parameters = %+v, want limit and query as query params", o.Parameters)
	}
}

func TestGatewayBodyStar(t *testing.T) {
	doc := gatewayDoc(t)
	o := op(t, doc, "/v1/users", "post")
	if o.RequestBody == nil {
		t.Fatal("no request body for body: \"*\"")
	}
	if ref := o.RequestBody.Content["application/json"].Schema.Ref; ref != "#/components/schemas/shop.v1.CreateUserRequest" {
		t.Errorf("body schema ref = %q, want the whole request message", ref)
	}
	if len(o.Parameters) != 0 {
		t.Errorf("parameters = %+v, want none: every field is in the body", o.Parameters)
	}
}

func TestGatewayBodyNamedFieldAndAdditionalBinding(t *testing.T) {
	doc := gatewayDoc(t)
	for _, method := range []string{"patch", "put"} {
		o := op(t, doc, "/v1/users/{user_id}", method)
		if o.RequestBody == nil {
			t.Fatalf("%s: no request body", method)
		}
		if ref := o.RequestBody.Content["application/json"].Schema.Ref; ref != "#/components/schemas/shop.v1.User" {
			t.Errorf("%s: body schema ref = %q, want the named field's type", method, ref)
		}
		if len(o.Parameters) != 1 || o.Parameters[0].Name != "user_id" {
			t.Errorf("%s: parameters = %+v, want just user_id", method, o.Parameters)
		}
	}
}

// "{name=users/*}" names the field "name"; the pattern after "=" is a
// gateway matching detail, not part of the OpenAPI path.
func TestGatewayPathTemplatePattern(t *testing.T) {
	doc := gatewayDoc(t)
	o := op(t, doc, "/v1/{name}", "delete")
	if len(o.Parameters) != 1 || o.Parameters[0].Name != "name" {
		t.Errorf("parameters = %+v, want the name path param", o.Parameters)
	}
}

func TestGatewayServerStreamingIsMarkedRealtime(t *testing.T) {
	doc := gatewayDoc(t)
	o := op(t, doc, "/v1/users", "get")
	if o.Realtime == "" {
		t.Error("a server-streaming binding should be marked realtime, not documented as one body")
	}
}

func TestGatewayUnannotatedRPCIsAbsent(t *testing.T) {
	doc := gatewayDoc(t)
	for path, methods := range doc.Paths {
		for method, o := range methods {
			if o.OperationID == "UserService_InternalPing" {
				t.Fatalf("unannotated RPC surfaced as %s %s", method, path)
			}
		}
	}
}

// A proto tree with no annotations yields an empty document, not an error:
// the service simply exposes nothing over HTTP.
func TestGatewayNoAnnotationsAtAll(t *testing.T) {
	doc, err := ScanGateway("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Paths) != 0 {
		t.Fatalf("paths = %v, want none: testdata/shop.proto has no http options", doc.Paths)
	}
}
