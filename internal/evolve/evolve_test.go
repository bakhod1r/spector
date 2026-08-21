package evolve

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// build makes a one-operation document so a test can state exactly the before
// and after it cares about.
func build(method, path string, op *core.Operation, schemas ...map[string]*core.Schema) *core.Document {
	d := core.NewDocument("t", "1")
	d.AddOperation(path, method, op)
	for _, set := range schemas {
		for name, s := range set {
			d.Components.Schemas[name] = s
		}
	}
	return d
}

func op(configure func(*core.Operation)) *core.Operation {
	o := core.NewOperation("")
	o.Responses = map[string]*core.Response{"200": {Description: "ok"}}
	if configure != nil {
		configure(o)
	}
	return o
}

// only asserts exactly one change of the given kind and severity, so a rule is
// tested in isolation rather than amid incidental noise.
func only(t *testing.T, changes []Change, severity, kind string) Change {
	t.Helper()
	var found []Change
	for _, c := range changes {
		if c.Kind == kind {
			found = append(found, c)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one %s change, got %d: %+v", kind, len(found), changes)
	}
	if found[0].Severity != severity {
		t.Errorf("severity = %q, want %q for %s", found[0].Severity, severity, kind)
	}
	return found[0]
}

func none(t *testing.T, changes []Change) {
	t.Helper()
	if len(changes) != 0 {
		t.Fatalf("want no changes, got %+v", changes)
	}
}

// ---- endpoints ----

func TestRemovedEndpointIsBreaking(t *testing.T) {
	old := build("get", "/users", op(nil))
	newDoc := core.NewDocument("t", "1")
	only(t, Compare(old, newDoc), Breaking, KindEndpointRemoved)
}

func TestAddedEndpointIsAnAddition(t *testing.T) {
	old := core.NewDocument("t", "1")
	newDoc := build("get", "/users", op(nil))
	only(t, Compare(old, newDoc), Addition, KindEndpointAdded)
}

func TestAnIdenticalDocumentHasNoChanges(t *testing.T) {
	mk := func() *core.Document {
		return build("post", "/users", op(func(o *core.Operation) {
			o.Parameters = []core.Parameter{{Name: "limit", In: "query", Schema: &core.Schema{Type: "integer"}}}
		}))
	}
	none(t, Compare(mk(), mk()))
}

// ---- responses ----

func TestRemovedStatusIsBreaking(t *testing.T) {
	old := build("get", "/users/{id}", op(func(o *core.Operation) {
		o.Responses["404"] = &core.Response{Description: "gone"}
	}))
	newDoc := build("get", "/users/{id}", op(nil)) // only 200
	only(t, Compare(old, newDoc), Breaking, KindStatusRemoved)
}

func TestAddedStatusIsCompatible(t *testing.T) {
	old := build("get", "/users/{id}", op(nil))
	newDoc := build("get", "/users/{id}", op(func(o *core.Operation) {
		o.Responses["404"] = &core.Response{Description: "gone"}
	}))
	only(t, Compare(old, newDoc), Compatible, KindStatusAdded)
}

func withBody(status string, schema *core.Schema) func(*core.Operation) {
	return func(o *core.Operation) {
		o.Responses = map[string]*core.Response{
			status: {Description: "ok", Content: map[string]core.MediaType{"application/json": {Schema: schema}}},
		}
	}
}

func obj(required []string, props map[string]*core.Schema) *core.Schema {
	return &core.Schema{Type: "object", Required: required, Properties: props}
}

func TestRemovedResponseFieldIsBreaking(t *testing.T) {
	old := build("get", "/u", op(withBody("200", obj([]string{"id"}, map[string]*core.Schema{
		"id": {Type: "integer"}, "name": {Type: "string"},
	}))))
	newDoc := build("get", "/u", op(withBody("200", obj([]string{"id"}, map[string]*core.Schema{
		"id": {Type: "integer"},
	}))))
	only(t, Compare(old, newDoc), Breaking, KindResponseFieldRemoved)
}

func TestResponseFieldBecomingOptionalIsBreaking(t *testing.T) {
	old := build("get", "/u", op(withBody("200", obj([]string{"id", "name"}, map[string]*core.Schema{
		"id": {Type: "integer"}, "name": {Type: "string"},
	}))))
	newDoc := build("get", "/u", op(withBody("200", obj([]string{"id"}, map[string]*core.Schema{
		"id": {Type: "integer"}, "name": {Type: "string"},
	}))))
	only(t, Compare(old, newDoc), Breaking, KindResponseFieldOptional)
}

func TestAddedResponseFieldIsCompatible(t *testing.T) {
	old := build("get", "/u", op(withBody("200", obj(nil, map[string]*core.Schema{"id": {Type: "integer"}}))))
	newDoc := build("get", "/u", op(withBody("200", obj(nil, map[string]*core.Schema{
		"id": {Type: "integer"}, "name": {Type: "string"},
	}))))
	only(t, Compare(old, newDoc), Compatible, KindResponseFieldAdded)
}

func TestResponseEnumWideningIsBreaking(t *testing.T) {
	old := build("get", "/u", op(withBody("200", obj(nil, map[string]*core.Schema{
		"status": {Type: "string", Enum: []any{"active"}},
	}))))
	newDoc := build("get", "/u", op(withBody("200", obj(nil, map[string]*core.Schema{
		"status": {Type: "string", Enum: []any{"active", "archived"}},
	}))))
	ch := only(t, Compare(old, newDoc), Breaking, KindResponseEnumWidened)
	if !strings.Contains(ch.Detail, "archived") {
		t.Errorf("detail = %q, want the new value named", ch.Detail)
	}
}

// ---- parameters ----

func TestNewRequiredParameterIsBreaking(t *testing.T) {
	old := build("get", "/u", op(nil))
	newDoc := build("get", "/u", op(func(o *core.Operation) {
		o.Parameters = []core.Parameter{{Name: "tenant", In: "query", Required: true, Schema: &core.Schema{Type: "string"}}}
	}))
	only(t, Compare(old, newDoc), Breaking, KindRequiredParamAdded)
}

func TestNewOptionalParameterIsCompatible(t *testing.T) {
	old := build("get", "/u", op(nil))
	newDoc := build("get", "/u", op(func(o *core.Operation) {
		o.Parameters = []core.Parameter{{Name: "cursor", In: "query", Schema: &core.Schema{Type: "string"}}}
	}))
	only(t, Compare(old, newDoc), Compatible, KindOptionalParamAdded)
}

func TestParameterBecomingRequiredIsBreaking(t *testing.T) {
	mk := func(required bool) *core.Document {
		return build("get", "/u", op(func(o *core.Operation) {
			o.Parameters = []core.Parameter{{Name: "limit", In: "query", Required: required, Schema: &core.Schema{Type: "integer"}}}
		}))
	}
	only(t, Compare(mk(false), mk(true)), Breaking, KindParamNowRequired)
}

func TestParameterBecomingOptionalIsCompatible(t *testing.T) {
	mk := func(required bool) *core.Document {
		return build("get", "/u", op(func(o *core.Operation) {
			o.Parameters = []core.Parameter{{Name: "limit", In: "query", Required: required, Schema: &core.Schema{Type: "integer"}}}
		}))
	}
	only(t, Compare(mk(true), mk(false)), Compatible, KindParamNowOptional)
}

func TestParameterTypeChangeIsBreaking(t *testing.T) {
	mk := func(typ string) *core.Document {
		return build("get", "/u", op(func(o *core.Operation) {
			o.Parameters = []core.Parameter{{Name: "id", In: "query", Schema: &core.Schema{Type: typ}}}
		}))
	}
	only(t, Compare(mk("string"), mk("integer")), Breaking, KindTypeChanged)
}

// ---- request body ----

func withReqBody(schema *core.Schema) func(*core.Operation) {
	return func(o *core.Operation) {
		o.RequestBody = &core.RequestBody{Content: map[string]core.MediaType{"application/json": {Schema: schema}}}
	}
}

func TestNewRequiredRequestFieldIsBreaking(t *testing.T) {
	old := build("post", "/u", op(withReqBody(obj(nil, map[string]*core.Schema{"name": {Type: "string"}}))))
	newDoc := build("post", "/u", op(withReqBody(obj([]string{"email"}, map[string]*core.Schema{
		"name": {Type: "string"}, "email": {Type: "string"},
	}))))
	only(t, Compare(old, newDoc), Breaking, KindRequestFieldRequired)
}

func TestNewOptionalRequestFieldIsCompatible(t *testing.T) {
	old := build("post", "/u", op(withReqBody(obj(nil, map[string]*core.Schema{"name": {Type: "string"}}))))
	newDoc := build("post", "/u", op(withReqBody(obj(nil, map[string]*core.Schema{
		"name": {Type: "string"}, "nickname": {Type: "string"},
	}))))
	only(t, Compare(old, newDoc), Compatible, KindRequestFieldAdded)
}

func TestExistingRequestFieldBecomingRequiredIsBreaking(t *testing.T) {
	mk := func(required []string) *core.Document {
		return build("post", "/u", op(withReqBody(obj(required, map[string]*core.Schema{"email": {Type: "string"}}))))
	}
	only(t, Compare(mk(nil), mk([]string{"email"})), Breaking, KindRequestFieldRequired)
}

func TestRequestEnumNarrowingIsBreaking(t *testing.T) {
	old := build("post", "/u", op(withReqBody(obj(nil, map[string]*core.Schema{
		"tier": {Type: "string", Enum: []any{"free", "pro", "enterprise"}},
	}))))
	newDoc := build("post", "/u", op(withReqBody(obj(nil, map[string]*core.Schema{
		"tier": {Type: "string", Enum: []any{"free", "pro"}},
	}))))
	ch := only(t, Compare(old, newDoc), Breaking, KindRequestEnumNarrowed)
	if !strings.Contains(ch.Detail, "enterprise") {
		t.Errorf("detail = %q, want the dropped value named", ch.Detail)
	}
}

// A request enum gaining a value is safe: the client's existing values still
// work, there are simply more of them.
func TestRequestEnumWideningIsNotBreaking(t *testing.T) {
	old := build("post", "/u", op(withReqBody(obj(nil, map[string]*core.Schema{
		"tier": {Type: "string", Enum: []any{"free"}},
	}))))
	newDoc := build("post", "/u", op(withReqBody(obj(nil, map[string]*core.Schema{
		"tier": {Type: "string", Enum: []any{"free", "pro"}},
	}))))
	for _, c := range Compare(old, newDoc) {
		if c.Severity == Breaking {
			t.Errorf("widening a request enum was reported as breaking: %+v", c)
		}
	}
}

// ---- refs and nesting ----

func TestChangesAreFoundThroughRefs(t *testing.T) {
	userV1 := map[string]*core.Schema{"User": obj([]string{"id", "name"}, map[string]*core.Schema{
		"id": {Type: "integer"}, "name": {Type: "string"},
	})}
	userV2 := map[string]*core.Schema{"User": obj([]string{"id"}, map[string]*core.Schema{
		"id": {Type: "integer"}, // name removed
	})}
	ref := &core.Schema{Ref: "#/components/schemas/User"}

	old := build("get", "/u", op(withBody("200", ref)), userV1)
	newDoc := build("get", "/u", op(withBody("200", ref)), userV2)
	only(t, Compare(old, newDoc), Breaking, KindResponseFieldRemoved)
}

// A schema that refers to itself must not hang the comparison.
func TestSelfReferentialSchemaTerminates(t *testing.T) {
	node := map[string]*core.Schema{"Node": obj([]string{"id"}, map[string]*core.Schema{
		"id":       {Type: "integer"},
		"children": {Type: "array", Items: &core.Schema{Ref: "#/components/schemas/Node"}},
	})}
	ref := &core.Schema{Ref: "#/components/schemas/Node"}
	old := build("get", "/tree", op(withBody("200", ref)), node)
	newDoc := build("get", "/tree", op(withBody("200", ref)), node)
	none(t, Compare(old, newDoc))
}

// ---- deprecation ----

func TestDeprecationIsCompatible(t *testing.T) {
	old := build("get", "/u", op(nil))
	newDoc := build("get", "/u", op(func(o *core.Operation) { o.Deprecated = true }))
	only(t, Compare(old, newDoc), Compatible, KindDeprecated)
}

// ---- ordering ----

func TestBreakingChangesSortFirst(t *testing.T) {
	old := build("get", "/a", op(nil))
	newDoc := core.NewDocument("t", "1")
	newDoc.AddOperation("/b", "get", op(nil))                      // addition
	newDoc.AddOperation("/c", "post", op(func(o *core.Operation) { // addition
		o.Parameters = []core.Parameter{{Name: "x", In: "query", Required: true, Schema: &core.Schema{Type: "string"}}}
	}))
	// /a removed is breaking.
	changes := Compare(old, newDoc)
	if len(changes) == 0 || changes[0].Severity != Breaking {
		t.Fatalf("first change = %+v, want a breaking one first", changes)
	}
}

func TestSummaryCounts(t *testing.T) {
	old := build("get", "/gone", op(nil))
	newDoc := build("get", "/added", op(nil))
	s := Summarize(Compare(old, newDoc))
	if s.Breaking != 1 || s.Addition != 1 {
		t.Errorf("summary = %+v, want 1 breaking and 1 addition", s)
	}
}

// ---- rendering ----

func changesFixture() []Change {
	old := build("get", "/gone", op(nil))
	newDoc := build("get", "/added", op(nil))
	return Compare(old, newDoc)
}

func TestTextRenderLeadsWithBreaking(t *testing.T) {
	out := Render(changesFixture())
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !strings.HasPrefix(lines[0], "BREAKING") {
		t.Errorf("first line = %q, want the breaking change first", lines[0])
	}
	if !strings.Contains(out, "1 breaking") {
		t.Errorf("no summary line:\n%s", out)
	}
}

func TestEmptyRenderSaysSo(t *testing.T) {
	if out := Render(nil); !strings.Contains(out, "No API changes") {
		t.Errorf("= %q", out)
	}
	if out := RenderMarkdown(nil); !strings.Contains(out, "No API changes") {
		t.Errorf("= %q", out)
	}
}

func TestJSONRenderCarriesSummaryAndChanges(t *testing.T) {
	data, err := RenderJSON(changesFixture())
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Summary Summary  `json:"summary"`
		Changes []Change `json:"changes"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.Summary.Breaking != 1 || len(r.Changes) != 2 {
		t.Errorf("summary = %+v, %d changes", r.Summary, len(r.Changes))
	}
}

func TestMarkdownGroupsBySeverity(t *testing.T) {
	out := RenderMarkdown(changesFixture())
	if !strings.Contains(out, "### ⚠️ Breaking") || !strings.Contains(out, "### Additions") {
		t.Errorf("markdown is not grouped:\n%s", out)
	}
}
