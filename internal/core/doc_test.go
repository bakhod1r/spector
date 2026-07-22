package core

import (
	"encoding/json"
	"testing"
)

func TestNewDocument(t *testing.T) {
	d := NewDocument("API", "2.0")
	if d.OpenAPI != "3.0.3" {
		t.Errorf("openapi = %q, want 3.0.3", d.OpenAPI)
	}
	if d.Info.Title != "API" || d.Info.Version != "2.0" {
		t.Errorf("info = %+v", d.Info)
	}
	// The maps must be usable immediately; adapters write into them directly.
	if d.Paths == nil || d.Components.Schemas == nil {
		t.Fatal("NewDocument left a nil map")
	}
}

func TestAddOperation(t *testing.T) {
	d := NewDocument("t", "1")
	d.AddOperation("/users", "get", NewOperation("listUsers"))
	d.AddOperation("/users", "post", NewOperation("createUser"))
	d.AddOperation("/items", "get", NewOperation("listItems"))

	if len(d.Paths["/users"]) != 2 {
		t.Errorf("/users has %d methods, want 2", len(d.Paths["/users"]))
	}
	if d.Paths["/users"]["get"].OperationID != "listUsers" {
		t.Errorf("wrong operation stored under /users get")
	}
	if len(d.Paths["/items"]) != 1 {
		t.Errorf("/items has %d methods, want 1", len(d.Paths["/items"]))
	}
}

// A second operation on the same path+method replaces the first rather than
// silently producing a duplicate key in the emitted JSON.
func TestAddOperationOverwrites(t *testing.T) {
	d := NewDocument("t", "1")
	d.AddOperation("/x", "get", NewOperation("first"))
	d.AddOperation("/x", "get", NewOperation("second"))
	if got := d.Paths["/x"]["get"].OperationID; got != "second" {
		t.Errorf("operationId = %q, want second", got)
	}
}

func TestNewOperationApliesOptionsInOrder(t *testing.T) {
	op := NewOperation("getUser",
		WithSummary("Get a user"),
		WithDescription("Long form"),
		WithParameter(Parameter{Name: "id", In: "path", Required: true, Schema: &Schema{Type: "string"}}),
		WithParameter(Parameter{Name: "q", In: "query"}),
		WithRequestBody(&RequestBody{Required: true}),
	)

	if op.OperationID != "getUser" {
		t.Errorf("operationId = %q", op.OperationID)
	}
	if op.Summary != "Get a user" || op.Description != "Long form" {
		t.Errorf("summary/description not applied: %+v", op)
	}
	if len(op.Parameters) != 2 || op.Parameters[0].Name != "id" || op.Parameters[1].Name != "q" {
		t.Errorf("parameters = %+v, want id then q", op.Parameters)
	}
	if op.RequestBody == nil || !op.RequestBody.Required {
		t.Errorf("request body not applied: %+v", op.RequestBody)
	}
	if op.Responses == nil {
		t.Error("Responses map was left nil")
	}
}

func TestNewOperationNoOptions(t *testing.T) {
	op := NewOperation("bare")
	if op.Responses == nil {
		t.Fatal("Responses map was left nil")
	}
	if len(op.Parameters) != 0 || op.RequestBody != nil {
		t.Errorf("bare operation carries unexpected fields: %+v", op)
	}
}

func TestSetResponse(t *testing.T) {
	op := NewOperation("x")
	op.SetResponse(200, NewResponse("ok"))
	op.SetResponse(404, NewResponse("missing"))

	if op.Responses["200"] == nil || op.Responses["200"].Description != "ok" {
		t.Errorf("200 response = %+v", op.Responses["200"])
	}
	if op.Responses["404"] == nil {
		t.Error("404 response missing")
	}
	// Statuses are string keys in OpenAPI, not numbers.
	if _, ok := op.Responses["200"]; !ok {
		t.Error("status was not stored as a string key")
	}
}

func TestNewResponseWithJSONBody(t *testing.T) {
	schema := &Schema{Ref: "#/components/schemas/User"}
	r := NewResponse("ok", WithJSONBody(schema))

	mt, ok := r.Content["application/json"]
	if !ok {
		t.Fatalf("no application/json content: %+v", r.Content)
	}
	if mt.Schema != schema {
		t.Errorf("schema = %+v, want the one passed in", mt.Schema)
	}
}

// A nil schema must leave Content absent so the response serialises without an
// empty content object.
func TestWithJSONBodyNilSchemaIsNoOp(t *testing.T) {
	r := NewResponse("no content", WithJSONBody(nil))
	if r.Content != nil {
		t.Errorf("Content = %+v, want nil for a nil schema", r.Content)
	}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `{"description":"no content"}` {
		t.Errorf("marshalled = %s, want no content key", got)
	}
}

func TestWithJSONBodyTwiceKeepsLast(t *testing.T) {
	first := &Schema{Type: "string"}
	second := &Schema{Type: "integer"}
	r := NewResponse("ok", WithJSONBody(first), WithJSONBody(second))
	if r.Content["application/json"].Schema != second {
		t.Error("second body did not replace the first")
	}
}

func TestNewGrpcDoc(t *testing.T) {
	d := NewGrpcDoc()
	if d == nil {
		t.Fatal("NewGrpcDoc returned nil")
	}
	// The console fetches grpc.json unconditionally and indexes into these, so
	// they must marshal as empty collections rather than null.
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("grpc doc does not round-trip: %v", err)
	}
	if len(d.Services) != 0 {
		t.Errorf("services = %d, want 0", len(d.Services))
	}
}

func TestNewGraphqlDoc(t *testing.T) {
	d := NewGraphqlDoc()
	if d == nil {
		t.Fatal("NewGraphqlDoc returned nil")
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("graphql doc does not round-trip: %v", err)
	}
	if len(d.Queries) != 0 {
		t.Errorf("queries = %d, want 0", len(d.Queries))
	}
}

// The whole document must survive a marshal/unmarshal cycle; it is served to
// the console as JSON and nothing else validates it.
func TestDocumentRoundTrips(t *testing.T) {
	d := NewDocument("API", "1")
	op := NewOperation("getUser",
		WithSummary("s"),
		WithParameter(Parameter{Name: "id", In: "path", Required: true, Schema: &Schema{Type: "string"}}),
	)
	op.SetResponse(200, NewResponse("ok", WithJSONBody(&Schema{Ref: "#/components/schemas/User"})))
	d.AddOperation("/users/{id}", "get", op)
	d.Components.Schemas["User"] = &Schema{
		Type:       "object",
		Properties: map[string]*Schema{"id": {Type: "integer"}},
	}

	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var back Document
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	got := back.Paths["/users/{id}"]["get"]
	if got == nil || got.OperationID != "getUser" {
		t.Fatalf("operation lost in round-trip: %+v", back.Paths)
	}
	if got.Responses["200"].Content["application/json"].Schema.Ref != "#/components/schemas/User" {
		t.Errorf("response schema ref lost: %+v", got.Responses["200"])
	}
}

// ---- security ----

// The scheme must serialise exactly as OpenAPI names the fields; a typo here
// silently produces a document tools reject.
func TestSecuritySchemeMarshalling(t *testing.T) {
	cases := []struct {
		name   string
		scheme SecurityScheme
		want   string
	}{
		{
			name:   "bearer",
			scheme: SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
			want:   `{"type":"http","scheme":"bearer","bearerFormat":"JWT"}`,
		},
		{
			name:   "basic",
			scheme: SecurityScheme{Type: "http", Scheme: "basic"},
			want:   `{"type":"http","scheme":"basic"}`,
		},
		{
			name:   "apiKey in header",
			scheme: SecurityScheme{Type: "apiKey", Name: "X-API-Key", In: "header"},
			want:   `{"type":"apiKey","name":"X-API-Key","in":"header"}`,
		},
		{
			name:   "with description",
			scheme: SecurityScheme{Type: "http", Scheme: "bearer", Description: "JWT from /login"},
			want:   `{"type":"http","scheme":"bearer","description":"JWT from /login"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.scheme)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.want {
				t.Errorf("got  %s\nwant %s", b, tc.want)
			}
		})
	}
}

// Scopes must survive as an empty array, not null: `{"bearerAuth": null}` is
// not a valid requirement object.
func TestSecurityRequirementMarshalsEmptyScopes(t *testing.T) {
	req := SecurityRequirement{"bearerAuth": []string{}}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `{"bearerAuth":[]}` {
		t.Errorf("got %s, want empty scopes as []", got)
	}
}

func TestServerMarshalling(t *testing.T) {
	b, err := json.Marshal(Server{URL: "https://api.example.com", Description: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `{"url":"https://api.example.com","description":"prod"}` {
		t.Errorf("got %s", got)
	}

	// Description is optional.
	b, _ = json.Marshal(Server{URL: "http://localhost:8080"})
	if got := string(b); got != `{"url":"http://localhost:8080"}` {
		t.Errorf("got %s, want no description key", got)
	}
}

// A document carrying servers and security must round-trip; it is served as
// JSON and nothing else validates it.
func TestDocumentWithSecurityRoundTrips(t *testing.T) {
	d := NewDocument("API", "1")
	d.Servers = []Server{{URL: "https://api.example.com"}}
	d.Security = []SecurityRequirement{{"bearerAuth": []string{}}}
	d.Components.SecuritySchemes = map[string]*SecurityScheme{
		"bearerAuth": {Type: "http", Scheme: "bearer"},
	}

	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var back Document
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Servers) != 1 || back.Servers[0].URL != "https://api.example.com" {
		t.Errorf("servers lost: %+v", back.Servers)
	}
	if len(back.Security) != 1 {
		t.Errorf("security lost: %+v", back.Security)
	}
	if s := back.Components.SecuritySchemes["bearerAuth"]; s == nil || s.Scheme != "bearer" {
		t.Errorf("scheme lost: %+v", back.Components.SecuritySchemes)
	}
}
