package admin

import (
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

// doc builds a document with the operations named, so each test can describe
// exactly the API shape it cares about.
func doc(ops ...[2]string) *core.Document {
	d := core.NewDocument("test", "1")
	for _, op := range ops {
		method, path := op[0], op[1]
		id := strings.ToLower(method) + strings.ReplaceAll(strings.Trim(path, "/"), "/", "")
		d.AddOperation(path, method, core.NewOperation(id))
	}
	return d
}

func find(t *testing.T, m Model, name string) Resource {
	t.Helper()
	for _, r := range m.Resources {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no resource %q in %v", name, slugs(m))
	return Resource{}
}

func slugs(m Model) []string {
	out := make([]string, 0, len(m.Resources))
	for _, r := range m.Resources {
		out = append(out, r.Name)
	}
	return out
}

// The point of the whole package: a panel must not offer an action the API does
// not have. A button that exists and does nothing is worse than no button.
func TestReadOnlyResourceOffersNoWriteActions(t *testing.T) {
	m := Build(doc([2]string{"get", "/users"}, [2]string{"get", "/users/{id}"}))
	r := find(t, m, "users")

	if r.Update != nil {
		t.Error("Edit is offered on an API with no update endpoint")
	}
	if r.Delete != nil {
		t.Error("Delete is offered on an API with no delete endpoint")
	}
	if r.Create != nil {
		t.Error("New is offered on an API with no create endpoint")
	}
	if got := r.Actions(); len(got) != 1 {
		t.Errorf("menu = %v, want only the detail action", got)
	}
}

func TestWriteActionsAppearWhenTheEndpointsExist(t *testing.T) {
	m := Build(doc(
		[2]string{"get", "/users"}, [2]string{"post", "/users"},
		[2]string{"get", "/users/{id}"}, [2]string{"put", "/users/{id}"},
		[2]string{"delete", "/users/{id}"},
	))
	r := find(t, m, "users")

	for name, a := range map[string]*Action{"Create": r.Create, "Update": r.Update, "Delete": r.Delete} {
		if a == nil {
			t.Errorf("%s is missing though the endpoint exists", name)
		}
	}
	if got := len(r.Actions()); got != 3 {
		t.Errorf("menu has %d entries, want view/edit/delete", got)
	}
}

// PATCH is an update as much as PUT is.
func TestPatchCountsAsUpdate(t *testing.T) {
	m := Build(doc([2]string{"get", "/users"}, [2]string{"patch", "/users/{id}"}))
	if find(t, m, "users").Update == nil {
		t.Error("PATCH did not register as an update")
	}
}

// A collection with no list has no master view to be the subject of. Inventing
// an empty table for it would suggest data exists where none was ever returned.
func TestCollectionWithoutListIsNotAResource(t *testing.T) {
	m := Build(doc([2]string{"post", "/events"}))
	if len(m.Resources) != 0 {
		t.Errorf("= %v, want no resources", slugs(m))
	}
}

// A menu with no entries should not render a three-dots button at all.
func TestResourceWithNoItemEndpointsHasNoMenu(t *testing.T) {
	m := Build(doc([2]string{"get", "/events"}))
	if find(t, m, "events").HasMenu() {
		t.Error("a list-only resource still claims a row menu")
	}
}

// A realtime endpoint has no body to tabulate and no form to fill.
func TestRealtimeEndpointsAreNotResources(t *testing.T) {
	d := doc([2]string{"get", "/stream"})
	d.Paths["/stream"]["get"].Realtime = "websocket"
	if m := Build(d); len(m.Resources) != 0 {
		t.Errorf("= %v, want a websocket endpoint excluded", slugs(m))
	}
}

// Labels are the team's own vocabulary. A POST to /carts whose handler is
// called openCart is "Open cart" — deriving "Create cart" from the method
// would overwrite a name someone chose deliberately.
func TestActionLabelsComeFromHandlerNames(t *testing.T) {
	d := core.NewDocument("t", "1")
	d.AddOperation("/carts", "get", core.NewOperation("listCarts"))
	d.AddOperation("/carts", "post", core.NewOperation("openCart"))
	d.AddOperation("/carts/{id}", "delete", core.NewOperation("abandonCart"))

	r := find(t, Build(d), "carts")
	if r.Create.Label != "Open Cart" {
		t.Errorf("create label = %q, want the handler's own name", r.Create.Label)
	}
	if r.Delete.Label != "Abandon Cart" {
		t.Errorf("delete label = %q, want the handler's own name", r.Delete.Label)
	}
	if r.Label != "Carts" {
		t.Errorf("resource label = %q", r.Label)
	}
}

// The item path's parameter is not always "id", and using the wrong one builds
// URLs that 404.
func TestNonStandardIDParamIsCarried(t *testing.T) {
	m := Build(doc([2]string{"get", "/users"}, [2]string{"get", "/users/{userId}"}))
	r := find(t, m, "users")
	if r.IDParam != "userId" {
		t.Errorf("IDParam = %q, want userId", r.IDParam)
	}
	if r.ItemPath != "/users/{userId}" {
		t.Errorf("ItemPath = %q", r.ItemPath)
	}
}

// Columns come from the list response, and the identifier leads.
func TestFieldsComeFromTheListSchemaWithIDFirst(t *testing.T) {
	d := core.NewDocument("t", "1")
	item := &core.Schema{Type: "object", Properties: map[string]*core.Schema{
		"name": {Type: "string"}, "id": {Type: "integer"}, "active": {Type: "boolean"},
	}}
	op := core.NewOperation("listUsers")
	op.SetResponse(200, core.NewResponse("ok", core.WithJSONBody(&core.Schema{Type: "array", Items: item})))
	d.AddOperation("/users", "get", op)

	r := find(t, Build(d), "users")
	if len(r.Fields) != 3 {
		t.Fatalf("fields = %+v", r.Fields)
	}
	if r.Fields[0].Name != "id" || !r.Fields[0].Primary {
		t.Errorf("first column = %+v, want the identifier", r.Fields[0])
	}
}

// A wrapped list is as common as a bare array, and the columns worth showing
// are the item's rather than the envelope's.
func TestWrappedListUnwrapsToTheItemSchema(t *testing.T) {
	d := core.NewDocument("t", "1")
	item := &core.Schema{Type: "object", Properties: map[string]*core.Schema{"id": {Type: "integer"}}}
	envelope := &core.Schema{Type: "object", Properties: map[string]*core.Schema{
		"items": {Type: "array", Items: item},
		"total": {Type: "integer"},
	}}
	op := core.NewOperation("listUsers")
	op.SetResponse(200, core.NewResponse("ok", core.WithJSONBody(envelope)))
	d.AddOperation("/users", "get", op)

	r := find(t, Build(d), "users")
	if len(r.Fields) != 1 || r.Fields[0].Name != "id" {
		t.Errorf("fields = %+v, want the item's own properties", r.Fields)
	}
}

// The form describes what the API accepts, including which fields it demands.
func TestFormFieldsAndRequirednessComeFromTheRequestBody(t *testing.T) {
	d := core.NewDocument("t", "1")
	d.AddOperation("/users", "get", core.NewOperation("listUsers"))
	body := &core.Schema{Type: "object", Required: []string{"email"},
		Properties: map[string]*core.Schema{
			"email": {Type: "string", Format: "email"},
			"tier":  {Type: "string", Enum: []any{"free", "pro"}},
		}}
	create := core.NewOperation("createUser", core.WithRequestBody(&core.RequestBody{
		Required: true, Content: map[string]core.MediaType{"application/json": {Schema: body}},
	}))
	d.AddOperation("/users", "post", create)

	r := find(t, Build(d), "users")
	byName := map[string]Field{}
	for _, f := range r.Form {
		byName[f.Name] = f
	}
	if !byName["email"].Required {
		t.Error("a required field is presented as optional")
	}
	if byName["tier"].Required {
		t.Error("an optional field is presented as required")
	}
	if got := byName["tier"].Enum; len(got) != 2 {
		t.Errorf("enum = %v, want the allowed values so the form is a select", got)
	}
}

// A $ref is the normal case in a generated document; failing to follow it would
// leave every resource with no columns.
func TestRefsAreResolved(t *testing.T) {
	d := core.NewDocument("t", "1")
	d.Components.Schemas["User"] = &core.Schema{Type: "object",
		Properties: map[string]*core.Schema{"id": {Type: "integer"}}}
	op := core.NewOperation("listUsers")
	op.SetResponse(200, core.NewResponse("ok", core.WithJSONBody(
		&core.Schema{Type: "array", Items: &core.Schema{Ref: "#/components/schemas/User"}})))
	d.AddOperation("/users", "get", op)

	if r := find(t, Build(d), "users"); len(r.Fields) != 1 {
		t.Errorf("fields = %+v, want the referenced schema's properties", r.Fields)
	}
}

// A guarded operation's required headers have to reach the generated code, or
// the panel cannot know it must forward a credential.
func TestRequiredHeadersReachTheAction(t *testing.T) {
	d := core.NewDocument("t", "1")
	op := core.NewOperation("listUsers",
		core.WithParameter(core.Parameter{Ref: "#/components/parameters/XSign"}),
		core.WithParameter(core.Parameter{Name: "X-Trace", In: "header"}))
	d.AddOperation("/users", "get", op)
	d.Components.Parameters = map[string]*core.Parameter{
		"XSign": {Name: "X-Sign", In: "header", Required: true},
	}

	got := find(t, Build(d), "users").List.Headers
	if len(got) != 1 || got[0] != "X-Sign" {
		t.Errorf("headers = %v, want only the required one, resolved through its $ref", got)
	}
}

// ---- generation ----

// The generated source has to compile, and gofmt failing is how the generator
// finds out that it does not.
func TestGeneratedSourceIsValidGo(t *testing.T) {
	d := doc([2]string{"get", "/users"}, [2]string{"post", "/users"},
		[2]string{"get", "/users/{id}"}, [2]string{"delete", "/users/{id}"})

	files, err := Generate(d, Options{ImportPath: "example.com/p/admin", Dir: "admin"})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"admin.go": false, "resources.go": false, "cmd/adminpanel/main.go": false,
		"templates/layout.html": false, "templates/list.html": false,
		"templates/detail.html": false, "templates/form.html": false,
		"templates/login.html": false,
	}
	for _, f := range files {
		if _, expected := want[f.Name]; !expected {
			t.Errorf("unexpected file %q", f.Name)
			continue
		}
		want[f.Name] = true
		if len(f.Data) == 0 {
			t.Errorf("%s is empty", f.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("%s was not generated", name)
		}
	}
}

// Without an import path the entrypoint would import something that does not
// resolve, so it is omitted rather than emitted broken.
func TestEntrypointIsSkippedWithoutAnImportPath(t *testing.T) {
	files, err := Generate(doc([2]string{"get", "/users"}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name, "cmd/") {
			t.Errorf("%s was generated with no import path to import", f.Name)
		}
	}
}

// A document with nothing to administer should say so, not write a panel with
// an empty sidebar.
func TestGenerateRefusesADocumentWithNoResources(t *testing.T) {
	if _, err := Generate(doc([2]string{"post", "/events"}), Options{}); err == nil {
		t.Fatal("generated a panel with no resources")
	}
}

// A foreign key links to another resource; an unrelated *Id does not.
func TestForeignKeysBecomeLinks(t *testing.T) {
	d := core.NewDocument("t", "1")
	// users, so orders.userId can point at it
	uop := core.NewOperation("listUsers")
	uop.SetResponse(200, core.NewResponse("ok", core.WithJSONBody(&core.Schema{Type: "array",
		Items: &core.Schema{Type: "object", Properties: map[string]*core.Schema{"id": {Type: "integer"}}}})))
	d.AddOperation("/users", "get", uop)
	oop := core.NewOperation("listOrders")
	oop.SetResponse(200, core.NewResponse("ok", core.WithJSONBody(&core.Schema{Type: "array",
		Items: &core.Schema{Type: "object", Properties: map[string]*core.Schema{
			"id": {Type: "integer"}, "userId": {Type: "integer"}, "randomId": {Type: "integer"},
		}}})))
	d.AddOperation("/orders", "get", oop)

	orders := find(t, Build(d), "orders")
	var userId, randomId, ownID Field
	for _, f := range orders.Fields {
		switch f.Name {
		case "userId":
			userId = f
		case "randomId":
			randomId = f
		case "id":
			ownID = f
		}
	}
	if userId.Ref != "users" {
		t.Errorf("userId.Ref = %q, want users", userId.Ref)
	}
	if randomId.Ref != "" {
		t.Errorf("randomId.Ref = %q, want none — no such resource", randomId.Ref)
	}
	if ownID.Ref != "" {
		t.Errorf("the item's own id links to itself: %q", ownID.Ref)
	}
}

func TestImageFieldsAreMarked(t *testing.T) {
	d := core.NewDocument("t", "1")
	op := core.NewOperation("listProducts")
	op.SetResponse(200, core.NewResponse("ok", core.WithJSONBody(&core.Schema{Type: "array",
		Items: &core.Schema{Type: "object", Properties: map[string]*core.Schema{
			"image": {Type: "string"}, "title": {Type: "string"},
		}}})))
	d.AddOperation("/products", "get", op)

	for _, f := range find(t, Build(d), "products").Fields {
		if f.Name == "image" && !f.Image {
			t.Error("image field not marked")
		}
		if f.Name == "title" && f.Image {
			t.Error("title wrongly marked as an image")
		}
	}
}

// A $ref property (price -> Money) must resolve to a type, or a form renders it
// as a text box and submits a string where the API wants an object.
func TestRefPropertyResolvesToObjectType(t *testing.T) {
	d := core.NewDocument("t", "1")
	d.Components.Schemas["Money"] = &core.Schema{Type: "object",
		Properties: map[string]*core.Schema{"amount": {Type: "number"}}}
	op := core.NewOperation("listProducts")
	op.SetResponse(200, core.NewResponse("ok", core.WithJSONBody(&core.Schema{Type: "array",
		Items: &core.Schema{Type: "object", Properties: map[string]*core.Schema{
			"price": {Ref: "#/components/schemas/Money"},
		}}})))
	d.AddOperation("/products", "get", op)

	for _, f := range find(t, Build(d), "products").Fields {
		if f.Name == "price" && f.Type != "object" {
			t.Errorf("price type = %q, want object", f.Type)
		}
	}
}

func TestHumanize(t *testing.T) {
	cases := map[string]string{
		"listUsers":  "List Users",
		"created_at": "Created At",
		"openCart":   "Open Cart",
		"id":         "Id",
		"":           "",
	}
	for in, want := range cases {
		if got := humanize(in); got != want {
			t.Errorf("humanize(%q) = %q, want %q", in, got, want)
		}
	}
}
