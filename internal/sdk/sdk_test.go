package sdk

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

// doc builds a small but representative document: a listed and a fetched
// resource, a creation with a body, query parameters, and a bodiless delete.
func doc() *core.Document {
	d := core.NewDocument("Users API", "1.0.0")
	d.Servers = []core.Server{{URL: "http://localhost:8080"}}

	userRef := &core.Schema{Ref: "#/components/schemas/User"}
	d.Components.Schemas["User"] = &core.Schema{
		Type: "object",
		Properties: map[string]*core.Schema{
			"id":    {Type: "integer"},
			"name":  {Type: "string"},
			"email": {Type: "string"},
		},
		Required: []string{"id", "name"},
	}
	d.Components.Schemas["CreateUserReq"] = &core.Schema{
		Type: "object",
		Properties: map[string]*core.Schema{
			"name": {Type: "string"},
		},
		Required: []string{"name"},
	}

	list := &core.Operation{
		Summary: "returns every user.",
		Parameters: []core.Parameter{
			{Name: "q", In: "query", Schema: &core.Schema{Type: "string"}},
			{Name: "limit", In: "query", Schema: &core.Schema{Type: "integer"}},
		},
		Responses: map[string]*core.Response{
			"200": {Content: map[string]core.MediaType{
				"application/json": {Schema: &core.Schema{Type: "array", Items: userRef}},
			}},
		},
	}
	get := &core.Operation{
		Parameters: []core.Parameter{
			{Name: "id", In: "path", Required: true, Schema: &core.Schema{Type: "string"}},
		},
		Responses: map[string]*core.Response{
			"200": {Content: map[string]core.MediaType{
				"application/json": {Schema: userRef},
			}},
		},
	}
	create := &core.Operation{
		RequestBody: &core.RequestBody{
			Required: true,
			Content: map[string]core.MediaType{
				"application/json": {Schema: &core.Schema{Ref: "#/components/schemas/CreateUserReq"}},
			},
		},
		Responses: map[string]*core.Response{
			"201": {Content: map[string]core.MediaType{
				"application/json": {Schema: userRef},
			}},
		},
	}
	del := &core.Operation{
		Parameters: []core.Parameter{
			{Name: "id", In: "path", Required: true, Schema: &core.Schema{Type: "string"}},
		},
		Responses: map[string]*core.Response{"204": {}},
	}

	d.AddOperation("/api/v1/users", "get", list)
	d.AddOperation("/api/v1/users/{id}", "get", get)
	d.AddOperation("/api/v1/users", "post", create)
	d.AddOperation("/api/v1/users/{id}", "delete", del)
	return d
}

func TestUnknownLanguageErrors(t *testing.T) {
	if _, err := Generate(doc(), Options{Lang: "rust"}); err == nil {
		t.Error("expected an error for an unknown language")
	}
}

func TestTSOutput(t *testing.T) {
	files, err := Generate(doc(), Options{Lang: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "client.ts" {
		t.Fatalf("files = %v", files)
	}
	src := string(files[0].Data)

	for _, want := range []string{
		"export interface User {",
		"export interface CreateUserReq {",
		"export class Client {",
		// Required vs optional membership.
		"id: number;",
		"email?: string;",
		// Derived names: operation per method+path.
		"getApiV1Users(",
		"getApiV1UsersById(",
		"postApiV1Users(",
		"deleteApiV1UsersById(",
		// Typed signatures.
		"body: CreateUserReq",
		"Promise<User[]>",
		// The default server lands in the constructor.
		`"http://localhost:8080"`,
		// A bodiless delete returns void.
		"deleteApiV1UsersById(id: string | number): Promise<void>",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("client.ts missing %q", want)
		}
	}
}

func TestGoOutputParses(t *testing.T) {
	files, err := Generate(doc(), Options{Lang: "go", Package: "usersapi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "client.go" {
		t.Fatalf("files = %v", files)
	}
	src := string(files[0].Data)

	// The output must be legal Go, not merely text that looks like it.
	fset := token.NewFileSet()
	if _, perr := parser.ParseFile(fset, "client.go", src, 0); perr != nil {
		t.Fatalf("generated Go does not parse: %v", perr)
	}

	for _, want := range []string{
		"package usersapi",
		"type User struct {",
		"type Client struct {",
		"func New(baseURL string) *Client {",
		"func (c *Client) GetApiV1Users(",
		"func (c *Client) GetApiV1UsersById(",
		"func (c *Client) PostApiV1Users(",
		"func (c *Client) DeleteApiV1UsersById(",
		"body CreateUserReq",
		"([]User, error)",
		// Path params are escaped into the URL.
		"url.PathEscape(id)",
		// Optional field carries omitempty; required does not.
		"`json:\"email,omitempty\"`",
		"`json:\"id\"`",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("client.go missing %q", want)
		}
	}
}

// operationId, when the document has one, wins over the derived name.
func TestOperationIDWins(t *testing.T) {
	d := doc()
	d.Paths["/api/v1/users"]["get"].OperationID = "listUsers"
	files, err := Generate(d, Options{Lang: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files[0].Data)
	if !strings.Contains(src, "listUsers(") {
		t.Error("operationId not used as the method name")
	}
	if strings.Contains(src, "getApiV1Users(") {
		t.Error("derived name used despite operationId")
	}
}
