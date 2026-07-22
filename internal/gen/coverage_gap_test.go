package gen

import (
	"testing"

	"github.com/user/specter/internal/core"
)

// An explicit operationId directive wins over the handler name.
func TestOperationIDDirectiveWins(t *testing.T) {
	route := core.Route{Method: "get", Path: "/users", HandlerName: "listUsers", OperationID: "ListAllUsers"}
	if got := operationID(route); got != "ListAllUsers" {
		t.Errorf("operationID = %q, want ListAllUsers", got)
	}
}

// Auth middleware with a scheme becomes a security requirement; a chain of two
// means both are required.
func TestSecurityFromMiddleware(t *testing.T) {
	routes := []core.Route{{
		Method: "get", Path: "/secure", HandlerName: "secure",
		Middleware: []core.Middleware{
			{Name: "Auth", Kind: "auth", Scheme: "bearerAuth"},
			{Name: "APIKey", Kind: "auth", Scheme: "apiKey"},
			{Name: "Logger"}, // no scheme; contributes nothing
		},
	}}
	doc := Build("t", "1", routes, nil)
	op := doc.Paths["/secure"]["get"]
	if len(op.Security) != 1 {
		t.Fatalf("security = %+v, want one requirement", op.Security)
	}
	req := op.Security[0]
	if _, ok := req["bearerAuth"]; !ok {
		t.Error("missing bearerAuth")
	}
	if _, ok := req["apiKey"]; !ok {
		t.Error("missing apiKey")
	}
}

// Query and header params declared on the route become string parameters.
func TestQueryAndHeaderParams(t *testing.T) {
	routes := []core.Route{{
		Method: "get", Path: "/search", HandlerName: "search",
		QueryParams:  []string{"q"},
		HeaderParams: []string{"X-Trace"},
	}}
	doc := Build("t", "1", routes, nil)
	op := doc.Paths["/search"]["get"]
	var haveQuery, haveHeader bool
	for _, p := range op.Parameters {
		if p.Name == "q" && p.In == "query" {
			haveQuery = true
		}
		if p.Name == "X-Trace" && p.In == "header" {
			haveHeader = true
		}
	}
	if !haveQuery || !haveHeader {
		t.Errorf("parameters = %+v, want q(query) and X-Trace(header)", op.Parameters)
	}
}

// Middleware headers become shared component parameters, defined once and
// referenced; a header the handler already reads is not duplicated. Middleware
// statuses become responses unless the handler documents that status itself.
func TestApplyMiddlewareHeadersAndStatuses(t *testing.T) {
	mw := []core.Middleware{{
		Name:     "Auth",
		Headers:  []string{"Authorization", "X-Trace"},
		Statuses: []int{401, 404},
	}}
	routes := []core.Route{
		{
			Method: "get", Path: "/a", HandlerName: "a",
			HeaderParams: []string{"X-Trace"}, // handler reads it too
			Middleware:   mw,
			Responses:    []core.RouteResponse{{Status: 200}, {Status: 404}},
		},
		{
			Method: "get", Path: "/b", HandlerName: "b",
			Middleware: mw, // same header again: reuses the component
		},
	}
	doc := Build("t", "1", routes, nil)

	opA := doc.Paths["/a"]["get"]
	var refs, traces int
	for _, p := range opA.Parameters {
		if p.Ref == paramRefPrefix+"Authorization" {
			refs++
		}
		if p.Name == "X-Trace" {
			traces++
		}
	}
	if refs != 1 {
		t.Errorf("Authorization refs = %d, want 1", refs)
	}
	if traces != 1 {
		t.Errorf("X-Trace params = %d, want 1 (no duplicate from middleware)", traces)
	}

	shared := doc.Components.Parameters["Authorization"]
	if shared == nil {
		t.Fatal("no shared Authorization parameter in components")
	}
	if shared.In != "header" || !shared.Required || shared.Description != "Required by Auth." {
		t.Errorf("shared parameter = %+v", shared)
	}

	// 401 added by middleware; 404 kept as the handler's own (no middleware text).
	if r := opA.Responses["401"]; r == nil || r.Description != "Unauthorized (from Auth)" {
		t.Errorf("401 = %+v", opA.Responses["401"])
	}
	if r := opA.Responses["404"]; r == nil || r.Description == "Not Found (from Auth)" {
		t.Errorf("404 should stay the handler's own, got %+v", opA.Responses["404"])
	}

	// Second route referenced the same Authorization component rather than
	// defining a second one; its X-Trace (not read by handler b) adds XTrace.
	if len(doc.Components.Parameters) != 2 || doc.Components.Parameters["XTrace"] == nil {
		t.Errorf("components.parameters = %+v, want Authorization and XTrace", doc.Components.Parameters)
	}
	opB := doc.Paths["/b"]["get"]
	var bRefs int
	for _, p := range opB.Parameters {
		if p.Ref == paramRefPrefix+"Authorization" {
			bRefs++
		}
	}
	if bRefs != 1 {
		t.Errorf("route b Authorization refs = %d, want 1", bRefs)
	}
}

// An array request body wraps the ref in an array schema.
func TestArrayRequestBody(t *testing.T) {
	routes := []core.Route{{
		Method: "post", Path: "/bulk", HandlerName: "bulk",
		RequestType: "CreateUserReq", RequestArray: true,
	}}
	doc := Build("t", "1", routes, sampleSchemas())
	op := doc.Paths["/bulk"]["post"]
	s := op.RequestBody.Content["application/json"].Schema
	if s.Type != "array" || s.Items == nil || s.Items.Ref != refPrefix+"CreateUserReq" {
		t.Errorf("schema = %+v, want array of CreateUserReq refs", s)
	}
}

// walk tolerates a nil schema hanging off a property map.
func TestWalkNilProperty(t *testing.T) {
	schemas := map[string]*core.Schema{
		"Odd": {Type: "object", Properties: map[string]*core.Schema{"gone": nil}},
	}
	routes := []core.Route{{Method: "get", Path: "/o", HandlerName: "o", ResponseType: "Odd"}}
	doc := Build("t", "1", routes, schemas) // must not panic
	if doc.Components.Schemas["Odd"] == nil {
		t.Error("Odd schema missing")
	}
}
