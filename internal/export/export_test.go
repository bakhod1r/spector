package export

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

func obj(props map[string]*core.Schema) *core.Schema {
	return &core.Schema{Type: "object", Properties: props}
}

func jsonBody(s *core.Schema) map[string]core.MediaType {
	return map[string]core.MediaType{"application/json": {Schema: s}}
}

// sampleDoc exercises most of the renderers at once: two groups, a ref'd
// parameter, a body, and both a success and an error response.
func sampleDoc() *core.Document {
	return &core.Document{
		Info:    core.Info{Title: "Test API", Version: "1.0.0"},
		Servers: []core.Server{{URL: "https://api.example.com", Description: "prod"}, {URL: "http://localhost:8080"}},
		Paths: map[string]map[string]*core.Operation{
			"/users/{id}": {"get": {
				Summary:    "Get a user",
				Tags:       []string{"users"},
				Parameters: []core.Parameter{{Ref: "#/components/parameters/UserID"}},
				Responses: map[string]*core.Response{
					"200": {Description: "ok", Content: jsonBody(obj(map[string]*core.Schema{"id": {Type: "integer"}}))},
					"404": {Description: "not found"},
				},
			}},
			"/users": {"post": {
				Summary:     "Create a user",
				Description: "Creates a user.",
				Tags:        []string{"users"},
				Parameters: []core.Parameter{
					{Name: "dry_run", In: "query", Schema: &core.Schema{Type: "boolean"}, Description: "skip\nwrites"},
					{Name: "X-Request-Id", In: "header", Required: true, Schema: &core.Schema{Type: "string"}},
					{Name: "ignored", In: "cookie"},
				},
				RequestBody: &core.RequestBody{Required: true, Content: jsonBody(obj(map[string]*core.Schema{"name": {Type: "string"}}))},
				Responses:   map[string]*core.Response{"201": {Description: "created"}},
			}},
			"/health": {"get": {Responses: map[string]*core.Response{}}},
		},
		Components: core.Components{
			Parameters: map[string]*core.Parameter{
				"UserID": {Name: "id", In: "path", Required: true, Schema: &core.Schema{Type: "integer"}, Description: "user id"},
			},
			SecuritySchemes: map[string]*core.SecurityScheme{
				"bearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
			},
		},
	}
}

func TestMarkdownFullDocument(t *testing.T) {
	out := string(Markdown(sampleDoc()))
	want := []string{
		"# Test API",
		"Version: `1.0.0`",
		"## Servers",
		"- `https://api.example.com` — prod",
		"- `http://localhost:8080`\n",
		"## Authentication",
		"- **bearerAuth** — bearer token in `Authorization: Bearer <token>` (JWT)",
		"## users",
		"### `GET /users/{id}`",
		"Get a user",
		"| `id` | path | integer | yes | user id |",
		"### `POST /users`",
		"| `dry_run` | query | boolean |  | skip writes |", // newline flattened, optional stays blank
		"| `X-Request-Id` | header | string | yes |  |",
		"Request body:",
		"```json",
		"Response `200` — ok",
		"Response `404` — not found",
		"## health", // no tags: falls back to the first path segment
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("markdown missing %q:\n%s", w, out)
		}
	}
	// The cookie parameter has no column in the table but must not break it.
	if strings.Count(out, "| Parameter | In | Type | Required | Description |") != 2 {
		t.Errorf("expected a parameter table per operation with params:\n%s", out)
	}
}

// The minimum document: no version, no servers, no auth, no paths.
func TestMarkdownEmptyDocument(t *testing.T) {
	out := string(Markdown(&core.Document{Info: core.Info{Title: "Bare"}}))
	if out != "# Bare\n\n" {
		t.Errorf("markdown = %q, want just the title heading", out)
	}
}

// Deprecation, description, per-operation security and an unresolvable ref all
// change the operation block.
func TestMarkdownOperationExtras(t *testing.T) {
	doc := &core.Document{
		Info: core.Info{Title: "X"},
		Paths: map[string]map[string]*core.Operation{
			"/old": {"delete": {
				Deprecated:  true,
				Summary:     "Old thing",
				Description: "Going away.",
				Security: []core.SecurityRequirement{
					{"apiKey": []string{}},
					{"bearerAuth": []string{}},
				},
				// A dangling ref resolves to an empty parameter and is dropped.
				Parameters: []core.Parameter{{Ref: "#/components/parameters/Missing"}},
				Responses:  map[string]*core.Response{"204": {}},
			}},
		},
	}
	out := string(Markdown(doc))
	for _, w := range []string{"> **Deprecated.**", "Old thing", "Going away.", "Auth required: apiKey, bearerAuth", "Response `204`:"} {
		if !strings.Contains(out, w) {
			t.Errorf("markdown missing %q:\n%s", w, out)
		}
	}
	// The dangling ref must not produce a table row.
	if strings.Contains(out, "| Parameter |") {
		t.Errorf("dangling ref should render no parameter table:\n%s", out)
	}
}

// A request body or response whose json media type has a nil schema has no
// example to show, and a non-json body is skipped entirely.
func TestMarkdownSkipsUntypedBodies(t *testing.T) {
	doc := &core.Document{
		Info: core.Info{Title: "X"},
		Paths: map[string]map[string]*core.Operation{
			"/a": {"post": {
				RequestBody: &core.RequestBody{Content: map[string]core.MediaType{"text/plain": {Schema: &core.Schema{Type: "string"}}}},
				Responses:   map[string]*core.Response{"200": {Content: map[string]core.MediaType{"application/json": {}}}},
			}},
			"/b": {"post": {
				RequestBody: &core.RequestBody{Content: map[string]core.MediaType{"application/json": {}}},
				Responses:   map[string]*core.Response{},
			}},
		},
	}
	out := string(Markdown(doc))
	if strings.Contains(out, "```json") {
		t.Errorf("no example should be rendered without a schema:\n%s", out)
	}
	if strings.Contains(out, "Request body:") {
		t.Errorf("untyped request bodies should be skipped:\n%s", out)
	}
}

// Every security scheme shape gets its own sentence, and an unknown type falls
// back to naming the type rather than dropping the scheme.
// A sample that cannot be marshalled (an example holding a func) must leave
// the section out rather than emit a broken code fence or panic.
func TestWriteExampleMarshalFailure(t *testing.T) {
	var b strings.Builder
	writeExample(&b, &core.Document{}, &core.Schema{Example: func() {}})
	if b.String() != "" {
		t.Errorf("wrote %q, want nothing", b.String())
	}
}

func TestWriteAuthSchemes(t *testing.T) {
	cases := []struct {
		name   string
		scheme *core.SecurityScheme
		want   string
	}{
		{"bearer no format", &core.SecurityScheme{Type: "http", Scheme: "bearer"}, "bearer token in `Authorization: Bearer <token>`\n"},
		{"basic", &core.SecurityScheme{Type: "http", Scheme: "basic"}, "HTTP basic auth"},
		{"apiKey", &core.SecurityScheme{Type: "apiKey", Name: "X-API-Key", In: "header"}, "API key `X-API-Key` in header"},
		{"oauth2", &core.SecurityScheme{Type: "oauth2"}, "— oauth2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			writeAuth(&b, &core.Document{Components: core.Components{
				SecuritySchemes: map[string]*core.SecurityScheme{"s": tc.scheme},
			}})
			if !strings.Contains(b.String(), tc.want) {
				t.Errorf("auth = %q, want it to contain %q", b.String(), tc.want)
			}
		})
	}
}

func TestPostmanFullDocument(t *testing.T) {
	data, err := Postman(sampleDoc())
	if err != nil {
		t.Fatal(err)
	}
	var col pmCollection
	if err := json.Unmarshal(data, &col); err != nil {
		t.Fatal(err)
	}
	if col.Info.Name != "Test API" {
		t.Errorf("name = %q", col.Info.Name)
	}
	if col.Auth == nil || col.Auth.Type != "bearer" {
		t.Fatalf("auth = %+v, want bearer", col.Auth)
	}
	if len(col.Item) != 2 {
		t.Fatalf("folders = %d, want 2 (users, health)", len(col.Item))
	}
	// Folder order follows path order: /health before /users*.
	if col.Item[0].Name != "health" || col.Item[1].Name != "users" {
		t.Errorf("folder order = %q, %q", col.Item[0].Name, col.Item[1].Name)
	}

	users := col.Item[1].Item
	if len(users) != 2 {
		t.Fatalf("users requests = %d, want 2", len(users))
	}
	// GET /users/{id} sorts before POST /users because paths sort first.
	get := users[0]
	if get.Name != "Create a user" && get.Name != "Get a user" {
		t.Errorf("request name = %q", get.Name)
	}
	var byName = map[string]pmItem{}
	for _, it := range users {
		byName[it.Name] = it
	}
	idReq := byName["Get a user"].Request
	if idReq == nil {
		t.Fatal("missing Get a user request")
	}
	// {id} becomes :id so Postman offers an editable path variable.
	if got := idReq.URL.Raw; got != "{{baseUrl}}/users/:id" {
		t.Errorf("url = %q", got)
	}
	if len(idReq.URL.Path) != 2 || idReq.URL.Path[1] != ":id" {
		t.Errorf("path segments = %v", idReq.URL.Path)
	}

	post := byName["Create a user"].Request
	if post.Method != "POST" {
		t.Errorf("method = %q", post.Method)
	}
	// Optional query params import disabled; header params become headers.
	if len(post.URL.Query) != 1 || post.URL.Query[0].Key != "dry_run" || !post.URL.Query[0].Disabled {
		t.Errorf("query = %+v", post.URL.Query)
	}
	var hdrs []string
	for _, h := range post.Header {
		hdrs = append(hdrs, h.Key)
	}
	if len(hdrs) != 2 || hdrs[0] != "X-Request-Id" || hdrs[1] != "Content-Type" {
		t.Errorf("headers = %v", hdrs)
	}
	if post.Body == nil || post.Body.Mode != "raw" || !strings.Contains(post.Body.Raw, "name") {
		t.Errorf("body = %+v", post.Body)
	}

	// Untagged, unsummarised /health falls back on both counts.
	health := col.Item[0].Item
	if len(health) != 1 || health[0].Name != "GET /health" {
		t.Errorf("health folder = %+v", health)
	}
}

// A required query parameter imports enabled — that is the distinction
// Disabled encodes.
func TestPostmanRequiredQueryEnabled(t *testing.T) {
	doc := &core.Document{Paths: map[string]map[string]*core.Operation{
		"/s": {"get": {Parameters: []core.Parameter{{Name: "q", In: "query", Required: true}}}},
	}}
	item := operationItem(doc, "{{baseUrl}}", "/s", "get", doc.Paths["/s"]["get"])
	if len(item.Request.URL.Query) != 1 || item.Request.URL.Query[0].Disabled {
		t.Errorf("required query should be enabled: %+v", item.Request.URL.Query)
	}
}

// Root path: Trim leaves an empty split element that must be skipped, and
// groupOf has no segment to name the folder after.
func TestPostmanRootPath(t *testing.T) {
	doc := &core.Document{Paths: map[string]map[string]*core.Operation{
		"/": {"get": {Responses: map[string]*core.Response{}}},
	}}
	data, err := Postman(doc)
	if err != nil {
		t.Fatal(err)
	}
	var col pmCollection
	if err := json.Unmarshal(data, &col); err != nil {
		t.Fatal(err)
	}
	if len(col.Item) != 1 || col.Item[0].Name != "root" {
		t.Fatalf("folders = %+v, want a single \"root\"", col.Item)
	}
	if got := col.Item[0].Item[0].Request.URL.Raw; got != "{{baseUrl}}/" {
		t.Errorf("url = %q", got)
	}
}

// An empty document still produces a valid, importable collection.
func TestPostmanEmptyDocument(t *testing.T) {
	data, err := Postman(&core.Document{})
	if err != nil {
		t.Fatal(err)
	}
	var col pmCollection
	if err := json.Unmarshal(data, &col); err != nil {
		t.Fatal(err)
	}
	if len(col.Item) != 0 || col.Auth != nil {
		t.Errorf("collection = %+v, want empty", col)
	}
}

// A request body with no JSON media type, or a JSON one with no schema, leaves
// the request without a body rather than emitting an empty one.
func TestPostmanSkipsUntypedBody(t *testing.T) {
	doc := &core.Document{}
	for _, rb := range []*core.RequestBody{
		{Content: map[string]core.MediaType{"text/plain": {Schema: &core.Schema{Type: "string"}}}},
		{Content: map[string]core.MediaType{"application/json": {}}},
	} {
		op := &core.Operation{RequestBody: rb}
		item := operationItem(doc, "{{baseUrl}}", "/a", "post", op)
		if item.Request.Body != nil {
			t.Errorf("body = %+v, want none", item.Request.Body)
		}
	}
}

func TestCollectionAuth(t *testing.T) {
	cases := []struct {
		name    string
		schemes map[string]*core.SecurityScheme
		want    string // pmAuth.Type, "" for nil
	}{
		{"none", nil, ""},
		{"bearer", map[string]*core.SecurityScheme{"a": {Type: "http", Scheme: "bearer"}}, "bearer"},
		{"basic", map[string]*core.SecurityScheme{"a": {Type: "http", Scheme: "basic"}}, "basic"},
		{"apiKey", map[string]*core.SecurityScheme{"a": {Type: "apiKey", Name: "X-Key", In: "query"}}, "apikey"},
		// apiKey with no location defaults to header, the common case.
		{"apiKey no in", map[string]*core.SecurityScheme{"a": {Type: "apiKey", Name: "X-Key"}}, "apikey"},
		// Unrepresentable schemes are skipped rather than mistranslated.
		{"oauth2 only", map[string]*core.SecurityScheme{"a": {Type: "oauth2"}}, ""},
		// Sorted names make the pick stable: "aaa" (basic) wins over "zzz".
		{"first sorted wins", map[string]*core.SecurityScheme{
			"zzz": {Type: "http", Scheme: "bearer"},
			"aaa": {Type: "http", Scheme: "basic"},
		}, "basic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectionAuth(&core.Document{Components: core.Components{SecuritySchemes: tc.schemes}})
			if tc.want == "" {
				if got != nil {
					t.Fatalf("auth = %+v, want nil", got)
				}
				return
			}
			if got == nil || got.Type != tc.want {
				t.Fatalf("auth = %+v, want type %q", got, tc.want)
			}
			if tc.name == "apiKey no in" {
				if got.APIKey[2]["value"] != "header" {
					t.Errorf("in = %v, want header", got.APIKey[2]["value"])
				}
			}
		})
	}
}

func TestResolveParam(t *testing.T) {
	target := &core.Parameter{Name: "id", In: "path"}
	doc := &core.Document{Components: core.Components{Parameters: map[string]*core.Parameter{
		"UserID": target,
		"Nil":    nil,
	}}}
	cases := []struct {
		name string
		in   core.Parameter
		want string // resolved Name
	}{
		{"inline", core.Parameter{Name: "q", In: "query"}, "q"},
		{"resolvable ref", core.Parameter{Ref: "#/components/parameters/UserID"}, "id"},
		{"dangling ref", core.Parameter{Ref: "#/components/parameters/Nope"}, ""},
		{"nil entry", core.Parameter{Ref: "#/components/parameters/Nil"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveParam(doc, tc.in).Name; got != tc.want {
				t.Errorf("name = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGroupOf(t *testing.T) {
	cases := []struct {
		name string
		op   *core.Operation
		path string
		want string
	}{
		{"first tag wins", &core.Operation{Tags: []string{"users", "admin"}}, "/x/y", "users"},
		{"first path segment", &core.Operation{}, "/users/{id}", "users"},
		{"skips leading variable", &core.Operation{}, "/{tenant}/orders", "orders"},
		{"root fallback", &core.Operation{}, "/", "root"},
		{"all variables", &core.Operation{}, "/{a}/{b}", "root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := groupOf(tc.op, tc.path); got != tc.want {
				t.Errorf("groupOf = %q, want %q", got, tc.want)
			}
		})
	}
}

// Methods come out in reader order, not alphabetical, so DELETE is not filed
// before GET.
func TestSortedMethodsAndPaths(t *testing.T) {
	ops := map[string]*core.Operation{"delete": {}, "get": {}, "patch": {}, "post": {}}
	got := sortedMethods(ops)
	want := []string{"get", "post", "patch", "delete"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedMethods = %v, want %v", got, want)
		}
	}
	doc := &core.Document{Paths: map[string]map[string]*core.Operation{"/b": nil, "/a": nil, "/c": nil}}
	paths := sortedPaths(doc)
	if paths[0] != "/a" || paths[2] != "/c" {
		t.Errorf("sortedPaths = %v", paths)
	}
}

// Both exporters must be byte-identical across runs despite map iteration.
func TestExportsAreDeterministic(t *testing.T) {
	doc := sampleDoc()
	for i := 0; i < 5; i++ {
		if string(Markdown(doc)) != string(Markdown(sampleDoc())) {
			t.Fatal("markdown output is not stable")
		}
		a, err := Postman(doc)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Postman(sampleDoc())
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatal("postman output is not stable")
		}
	}
}
