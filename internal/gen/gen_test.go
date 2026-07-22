package gen

import "testing"

import "github.com/user/specter/internal/core"

func sampleSchemas() map[string]*core.Schema {
	return map[string]*core.Schema{
		"User": {Type: "object", Properties: map[string]*core.Schema{
			"id":      {Type: "integer"},
			"profile": {Ref: refPrefix + "Profile"},
		}},
		"Profile": {Type: "object", Properties: map[string]*core.Schema{
			"bio": {Type: "string"},
		}},
		"CreateUserReq": {Type: "object", Properties: map[string]*core.Schema{
			"name": {Type: "string"},
		}},
		"Unused": {Type: "object"},
	}
}

func TestBuild(t *testing.T) {
	routes := []core.Route{
		{Method: "get", Path: "/users/{id}", HandlerName: "getUser", ResponseType: "User"},
		{Method: "post", Path: "/users", HandlerName: "createUser", RequestType: "CreateUserReq", ResponseType: "User"},
	}
	doc := Build("Test API", "1.0.0", routes, sampleSchemas())

	if doc.OpenAPI != "3.0.3" {
		t.Errorf("openapi = %q", doc.OpenAPI)
	}

	op := doc.Paths["/users/{id}"]["get"]
	if op == nil {
		t.Fatal("missing GET /users/{id}")
	}
	if len(op.Parameters) != 1 || op.Parameters[0].Name != "id" || op.Parameters[0].In != "path" {
		t.Errorf("path params = %+v", op.Parameters)
	}
	if op.Responses["200"].Content["application/json"].Schema.Ref != refPrefix+"User" {
		t.Errorf("response ref = %+v", op.Responses["200"])
	}

	post := doc.Paths["/users"]["post"]
	if post.RequestBody == nil || post.RequestBody.Content["application/json"].Schema.Ref != refPrefix+"CreateUserReq" {
		t.Errorf("request body = %+v", post.RequestBody)
	}

	for _, want := range []string{"User", "Profile", "CreateUserReq"} {
		if doc.Components.Schemas[want] == nil {
			t.Errorf("missing schema %q in components", want)
		}
	}
	if doc.Components.Schemas["Unused"] != nil {
		t.Error("Unused schema should not be in components")
	}
}

func TestBuildStatusCodes(t *testing.T) {
	routes := []core.Route{{
		Method:      "post",
		Path:        "/users",
		HandlerName: "createUser",
		Summary:     "Create a user",
		Description: "Creates and returns a new user.",
		Responses: []core.RouteResponse{
			{Status: 201, Type: "User"},
			{Status: 404},
		},
	}}
	doc := Build("T", "1", routes, sampleSchemas())
	op := doc.Paths["/users"]["post"]

	if op.Summary != "Create a user" || op.Description != "Creates and returns a new user." {
		t.Errorf("summary/description = %q / %q", op.Summary, op.Description)
	}
	if _, ok := op.Responses["200"]; ok {
		t.Error("did not expect a synthetic 200 response")
	}
	created := op.Responses["201"]
	if created == nil || created.Description != "Created" ||
		created.Content["application/json"].Schema.Ref != refPrefix+"User" {
		t.Errorf("201 = %+v", created)
	}
	if nf := op.Responses["404"]; nf == nil || nf.Description != "Not Found" || nf.Content != nil {
		t.Errorf("404 = %+v", nf)
	}
}

func TestBuildResponseWithoutType(t *testing.T) {
	routes := []core.Route{
		{Method: "delete", Path: "/users/{id}", HandlerName: "deleteUser"},
	}
	doc := Build("T", "1", routes, nil)
	resp := doc.Paths["/users/{id}"]["delete"].Responses["200"]
	if resp == nil || resp.Description != "OK" {
		t.Errorf("expected default 200 OK, got %+v", resp)
	}
}
