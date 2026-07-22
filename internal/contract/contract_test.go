package contract

import (
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

// doc builds a small but realistic document: a list, a fetch by id, and a
// create with a body — enough for every question the planner has to answer.
func doc() *core.Document {
	d := core.NewDocument("Shop", "1.0")
	d.Servers = []core.Server{{URL: "https://api.example.com"}}
	d.Components.Schemas["User"] = &core.Schema{
		Type:     "object",
		Required: []string{"id", "name"},
		Properties: map[string]*core.Schema{
			"id":   {Type: "integer"},
			"name": {Type: "string"},
		},
	}

	list := core.NewOperation("listUsers")
	list.Summary = "List users"
	list.Parameters = []core.Parameter{
		{Name: "limit", In: "query", Required: true, Schema: &core.Schema{Type: "integer"}},
		{Name: "cursor", In: "query", Schema: &core.Schema{Type: "string"}},
		{Name: "X-Tenant", In: "header", Required: true, Schema: &core.Schema{Type: "string"}},
	}
	list.Responses = map[string]*core.Response{
		"200": {Description: "ok", Content: map[string]core.MediaType{
			"application/json": {Schema: &core.Schema{Type: "array", Items: &core.Schema{Ref: "#/components/schemas/User"}}},
		}},
	}
	d.AddOperation("/users", "get", list)

	get := core.NewOperation("getUser")
	get.Parameters = []core.Parameter{
		{Name: "id", In: "path", Required: true, Schema: &core.Schema{Type: "integer"}},
	}
	get.Responses = map[string]*core.Response{
		"200": {Description: "ok", Content: map[string]core.MediaType{
			"application/json": {Schema: &core.Schema{Ref: "#/components/schemas/User"}},
		}},
		"404": {Description: "not found"},
	}
	d.AddOperation("/users/{id}", "get", get)

	create := core.NewOperation("createUser")
	create.RequestBody = &core.RequestBody{Required: true, Content: map[string]core.MediaType{
		"application/json": {Schema: &core.Schema{Ref: "#/components/schemas/User"}},
	}}
	create.Responses = map[string]*core.Response{
		"201": {Description: "created"},
	}
	d.AddOperation("/users", "post", create)

	return d
}

func plans(t *testing.T, d *core.Document) []request {
	t.Helper()
	got := requests(d, Options{})
	if len(got) == 0 {
		t.Fatal("no requests were planned")
	}
	return got
}

func find(t *testing.T, rs []request, method, rawPath string) request {
	t.Helper()
	for _, r := range rs {
		if r.Method == method && r.RawPath == rawPath {
			return r
		}
	}
	t.Fatalf("no %s %s in the plan", method, rawPath)
	return request{}
}

// ---- planning ----

// Every documented operation must be exercised. An artefact that quietly skips
// endpoints gives the same green result as one that checks them, which is the
// failure this package exists to prevent.
func TestEveryOperationIsPlanned(t *testing.T) {
	got := plans(t, doc())
	if len(got) != 3 {
		t.Fatalf("planned %d requests, want 3: %+v", len(got), got)
	}
}

// A request against /users/{id} literally is a 404 at best. Path parameters
// are filled from the schema so the artefact is runnable as written.
func TestPathParametersAreFilled(t *testing.T) {
	r := find(t, plans(t, doc()), "GET", "/users/{id}")
	if strings.Contains(r.Path, "{") {
		t.Errorf("path = %q, still holds a placeholder", r.Path)
	}
	if r.Path != "/users/1" {
		t.Errorf("path = %q, want an integer sample", r.Path)
	}
}

// Required parameters are what the API refuses without; optional ones are
// noise in a generated request, and a wrong guess makes the call fail for a
// reason that has nothing to do with the contract.
func TestOnlyRequiredParametersAreSent(t *testing.T) {
	r := find(t, plans(t, doc()), "GET", "/users")
	if len(r.Query) != 1 || r.Query[0].Key != "limit" {
		t.Errorf("query = %+v, want only the required limit", r.Query)
	}
	if len(r.Headers) != 1 || r.Headers[0].Key != "X-Tenant" {
		t.Errorf("headers = %+v, want only the required X-Tenant", r.Headers)
	}
}

// The body has to satisfy the schema it came from, or the request fails
// validation before it ever reaches the contract being tested.
func TestRequestBodyIsSampledFromTheSchema(t *testing.T) {
	r := find(t, plans(t, doc()), "POST", "/users")
	if len(r.Body) == 0 {
		t.Fatal("no body was generated for an operation that requires one")
	}
	for _, want := range []string{`"id"`, `"name"`} {
		if !strings.Contains(string(r.Body), want) {
			t.Errorf("body %s does not carry %s", r.Body, want)
		}
	}
}

// An operation with no request body must not be given one.
func TestNoBodyWhereNoneIsDocumented(t *testing.T) {
	r := find(t, plans(t, doc()), "GET", "/users")
	if len(r.Body) != 0 {
		t.Errorf("body = %s, want none", r.Body)
	}
}

// Every documented status is a legitimate answer. A test that demanded 200
// would fail on a 404 the document itself promises.
func TestAllDocumentedStatusesAreAccepted(t *testing.T) {
	r := find(t, plans(t, doc()), "GET", "/users/{id}")
	if len(r.Statuses) != 2 || r.Statuses[0] != 200 || r.Statuses[1] != 404 {
		t.Errorf("statuses = %v, want [200 404] in order", r.Statuses)
	}
}

// Regenerating must not churn a review. Map iteration is random, so order is
// imposed rather than inherited.
func TestOrderIsStable(t *testing.T) {
	first := requests(doc(), Options{})
	for i := 0; i < 20; i++ {
		next := requests(doc(), Options{})
		for j := range first {
			if first[j].Name != next[j].Name {
				t.Fatalf("order changed between runs: %s then %s", first[j].Name, next[j].Name)
			}
		}
	}
	// Path first, then method, so the two /users entries sit together.
	if first[0].RawPath != "/users" || first[0].Method != "GET" {
		t.Errorf("first = %s %s, want GET /users", first[0].Method, first[0].RawPath)
	}
	if first[1].Method != "POST" {
		t.Errorf("second = %s, want POST /users", first[1].Method)
	}
}

// ---- Generate ----

func generated(t *testing.T, opts Options) map[string][]byte {
	t.Helper()
	files, err := Generate(doc(), opts)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range files {
		out[f.Name] = f.Data
	}
	return out
}

func TestGenerateWritesEveryFormatByDefault(t *testing.T) {
	files := generated(t, Options{})
	for _, name := range []string{"requests.http", "contract_test.go", "check.go", "smoke.sh"} {
		if len(files[name]) == 0 {
			t.Errorf("%s was not generated; got %v", name, keys(files))
		}
	}
}

func TestFormatsSelectWhatIsWritten(t *testing.T) {
	files := generated(t, Options{Formats: []string{"http"}})
	if len(files["requests.http"]) == 0 {
		t.Error("requests.http was not generated")
	}
	if len(files) != 1 {
		t.Errorf("files = %v, want only the .http file", keys(files))
	}
}

// A document with no paths has nothing to exercise, and writing empty
// artefacts that always pass would be worse than saying so.
func TestEmptyDocumentIsAnError(t *testing.T) {
	if _, err := Generate(core.NewDocument("empty", "1"), Options{}); err == nil {
		t.Error("expected an error for a document with no operations")
	}
}

func TestUnknownFormatIsAnError(t *testing.T) {
	if _, err := Generate(doc(), Options{Formats: []string{"postman"}}); err == nil {
		t.Error("expected an error for an unknown format")
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---- .http ----

func httpFile(t *testing.T) string {
	t.Helper()
	return string(generated(t, Options{Formats: []string{"http"}})["requests.http"])
}

// The point of the .http file is that it runs as written. A placeholder left
// in a URL, or a variable the reader has to invent, defeats it.
func TestHTTPFileIsRunnableAsWritten(t *testing.T) {
	out := httpFile(t)
	if !strings.Contains(out, "@baseUrl = https://api.example.com") {
		t.Errorf("base URL is not taken from the document's server:\n%s", out)
	}
	if strings.Contains(out, "{id}") {
		t.Errorf("a path placeholder survived into the request:\n%s", out)
	}
	if !strings.Contains(out, "GET {{baseUrl}}/users/1") {
		t.Errorf("the filled path is missing:\n%s", out)
	}
}

func TestHTTPFileCarriesRequiredParameters(t *testing.T) {
	out := httpFile(t)
	if !strings.Contains(out, "GET {{baseUrl}}/users?limit=1") {
		t.Errorf("required query parameter missing:\n%s", out)
	}
	if !strings.Contains(out, "X-Tenant: string") {
		t.Errorf("required header missing:\n%s", out)
	}
}

func TestHTTPFileSendsABodyWithItsContentType(t *testing.T) {
	out := httpFile(t)
	if !strings.Contains(out, "POST {{baseUrl}}/users") {
		t.Fatalf("the create request is missing:\n%s", out)
	}
	if !strings.Contains(out, "Content-Type: application/json") {
		t.Errorf("a body was sent without its content type:\n%s", out)
	}
}

// Each block is titled with what the endpoint does, because a file of forty
// bare URLs is not something anyone reads.
func TestHTTPBlocksAreNamed(t *testing.T) {
	if out := httpFile(t); !strings.Contains(out, "### List users") {
		t.Errorf("the summary is not used as the block title:\n%s", out)
	}
}

// An API with no declared scheme must not grow an Authorization header out of
// nowhere: sending one changes what is being tested.
func TestHTTPOmitsAuthWhenNoneIsDeclared(t *testing.T) {
	if out := httpFile(t); strings.Contains(out, "Authorization") {
		t.Errorf("auth appeared without a declared scheme:\n%s", out)
	}
}

func TestHTTPCarriesTheDeclaredScheme(t *testing.T) {
	d := doc()
	d.Components.SecuritySchemes = map[string]*core.SecurityScheme{
		"bearerAuth": {Type: "http", Scheme: "bearer"},
	}
	files, err := Generate(d, Options{Formats: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	out := string(files[0].Data)
	if !strings.Contains(out, "@token =") {
		t.Errorf("no token variable was declared:\n%s", out)
	}
	if !strings.Contains(out, "Authorization: Bearer {{token}}") {
		t.Errorf("the bearer header is missing:\n%s", out)
	}
}

// An API key goes in the header the document names, not in a guessed one.
func TestHTTPCarriesAnAPIKeyScheme(t *testing.T) {
	d := doc()
	d.Components.SecuritySchemes = map[string]*core.SecurityScheme{
		"apiKeyAuth": {Type: "apiKey", Name: "X-API-Key", In: "header"},
	}
	files, err := Generate(d, Options{Formats: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	if out := string(files[0].Data); !strings.Contains(out, "X-API-Key: {{apiKey}}") {
		t.Errorf("the declared key header is missing:\n%s", out)
	}
}

// ---- Go tests ----

func goFiles(t *testing.T) (test, check string) {
	t.Helper()
	files := generated(t, Options{Formats: []string{"go"}})
	return string(files["contract_test.go"]), string(files["check.go"])
}

// The generated file has to compile, and gofmt parses before it formats — so
// Generate succeeding at all is most of this test. The build tag is the rest:
// without it these network tests would run in every `go test ./...`.
func TestGoTestsAreTaggedAndCompile(t *testing.T) {
	test, check := goFiles(t)
	for name, src := range map[string]string{"contract_test.go": test, "check.go": check} {
		if !strings.HasPrefix(src, "//go:build contract") {
			t.Errorf("%s does not carry the contract build tag:\n%s", name, firstLine(src))
		}
	}
}

func TestOneTestPerEndpoint(t *testing.T) {
	test, _ := goFiles(t)
	for _, want := range []string{"func TestListUsers(", "func TestGetUser(", "func TestCreateUser("} {
		if !strings.Contains(test, want) {
			t.Errorf("%s missing from the generated tests", want)
		}
	}
}

// Every documented status is accepted. Demanding 200 would fail on the 404 the
// document itself promises.
func TestGeneratedTestAcceptsEveryDocumentedStatus(t *testing.T) {
	test, _ := goFiles(t)
	if !strings.Contains(test, "expectStatus(t, what, res, 200, 404)") {
		t.Errorf("the documented statuses are not all accepted:\n%s", test)
	}
}

// A response with no documented body has nothing to check, and inventing a
// shape assertion for it would fail correct code.
func TestNoShapeCheckWithoutADocumentedBody(t *testing.T) {
	test, _ := goFiles(t)
	created := section(test, "func TestCreateUser(")
	if strings.Contains(created, "expectShape") {
		t.Errorf("a shape check was generated for a 201 with no body:\n%s", created)
	}
}

func TestShapeCheckWhereABodyIsDocumented(t *testing.T) {
	test, _ := goFiles(t)
	listed := section(test, "func TestListUsers(")
	if !strings.Contains(listed, "expectShape") {
		t.Errorf("no shape check for a documented array response:\n%s", listed)
	}
}

// The checks resolve $ref at run time, so the component schemas have to travel
// with the generated code.
func TestComponentSchemasTravelWithTheTests(t *testing.T) {
	test, _ := goFiles(t)
	if !strings.Contains(test, "componentsJSON") || !strings.Contains(test, "User") {
		t.Errorf("the component schemas are not carried:\n%s", test)
	}
}

// Two endpoints can reasonably share a summary; two Go functions cannot share
// a name.
func TestDuplicateNamesAreMadeUnique(t *testing.T) {
	d := core.NewDocument("t", "1")
	for _, path := range []string{"/a", "/b"} {
		op := core.NewOperation("")
		op.Summary = "Delete"
		op.Responses = map[string]*core.Response{"204": {Description: "gone"}}
		d.AddOperation(path, "delete", op)
	}
	files, err := Generate(d, Options{Formats: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files[0].Data)
	if !strings.Contains(src, "func TestDelete(") || !strings.Contains(src, "func TestDelete2(") {
		t.Errorf("duplicate names were not disambiguated:\n%s", src)
	}
}

// ---- smoke.sh ----

func TestCurlChecksEveryEndpointAgainstItsStatuses(t *testing.T) {
	out := string(generated(t, Options{Formats: []string{"curl"}})["smoke.sh"])
	if !strings.HasPrefix(out, "#!/bin/sh") {
		t.Errorf("no shebang:\n%s", firstLine(out))
	}
	if !strings.Contains(out, "check 'GET' '/users/1' '200 404'") {
		t.Errorf("the documented statuses are not checked:\n%s", out)
	}
	if !strings.Contains(out, "check 'POST' '/users'") {
		t.Errorf("the create endpoint is not called:\n%s", out)
	}
	if !strings.Contains(out, "exit 1") {
		t.Errorf("a failing endpoint does not fail the script:\n%s", out)
	}
}

// A body has to survive being a shell argument, which a pretty-printed one
// does not.
func TestCurlBodyIsOneLine(t *testing.T) {
	out := string(generated(t, Options{Formats: []string{"curl"}})["smoke.sh"])
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "check 'POST'") && strings.Contains(line, `{"id"`) {
			return
		}
	}
	t.Errorf("the POST body was not inlined:\n%s", out)
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// section returns the generated function starting at marker, so an assertion
// about one endpoint is not satisfied by another endpoint's code.
func section(src, marker string) string {
	i := strings.Index(src, marker)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest, "\nfunc "); j > 0 {
		return rest[:j]
	}
	return rest
}

// A path parameter is an identifier, and adapters that cannot see a type
// document it as a string. Sampling that schema the ordinary way produces
// /users/string, which is a request no API answers and no reader believes.
func TestStringPathParametersGetAnIdentifierShapedValue(t *testing.T) {
	d := core.NewDocument("t", "1")
	op := core.NewOperation("getThing")
	op.Parameters = []core.Parameter{
		{Name: "id", In: "path", Required: true, Schema: &core.Schema{Type: "string"}},
	}
	op.Responses = map[string]*core.Response{"200": {Description: "ok"}}
	d.AddOperation("/things/{id}", "get", op)

	r := requests(d, Options{})[0]
	if r.Path != "/things/1" {
		t.Errorf("path = %q, want an identifier rather than the word string", r.Path)
	}
}

// A parameter the document constrains keeps its documented value: an enum or a
// format says what the endpoint accepts, and overriding it would send something
// the API rejects for a reason unrelated to the contract.
func TestConstrainedPathParametersKeepTheirDocumentedValue(t *testing.T) {
	d := core.NewDocument("t", "1")
	op := core.NewOperation("getByStatus")
	op.Parameters = []core.Parameter{
		{Name: "status", In: "path", Required: true, Schema: &core.Schema{Type: "string", Enum: []any{"active", "archived"}}},
	}
	op.Responses = map[string]*core.Response{"200": {Description: "ok"}}
	d.AddOperation("/things/{status}", "get", op)

	if r := requests(d, Options{})[0]; r.Path != "/things/active" {
		t.Errorf("path = %q, want the first documented enum value", r.Path)
	}
}
