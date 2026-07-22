package gen

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/user/specter/internal/core"
)

// buildWith runs Build over a single route whose response is the named schema,
// so the schema reaches the document and the prune pass sees it.
func buildWith(t *testing.T, schemas map[string]*core.Schema, root string) *core.Document {
	t.Helper()
	routes := []core.Route{{
		Method:       "get",
		Path:         "/x",
		HandlerName:  "getX",
		ResponseType: root,
	}}
	return Build("t", "1", routes, schemas)
}

// Embedding a type the scan never saw must not leave a reference to a schema
// that is absent from the document.
func TestPruneDanglingAllOfRef(t *testing.T) {
	doc := buildWith(t, map[string]*core.Schema{
		"T": {
			Type:       "object",
			Properties: map[string]*core.Schema{"name": {Type: "string"}},
			AllOf:      []*core.Schema{{Ref: refPrefix + "Mutex"}},
		},
	}, "T")

	got := doc.Components.Schemas["T"]
	if got == nil {
		t.Fatal("T missing from the document")
	}
	for _, sub := range got.AllOf {
		if strings.HasSuffix(sub.Ref, "Mutex") {
			t.Errorf("dangling allOf $ref survived: %v", sub.Ref)
		}
	}
	assertNoDanglingRefs(t, doc)
}

// A reference that does resolve must be left alone.
func TestPruneKeepsResolvableAllOfRef(t *testing.T) {
	doc := buildWith(t, map[string]*core.Schema{
		"Base": {Type: "object", Properties: map[string]*core.Schema{"id": {Type: "integer"}}},
		"T":    {Type: "object", AllOf: []*core.Schema{{Ref: refPrefix + "Base"}}},
	}, "T")

	got := doc.Components.Schemas["T"]
	if len(got.AllOf) != 1 || got.AllOf[0].Ref != refPrefix+"Base" {
		t.Errorf("allOf = %+v, want the Base ref kept", got.AllOf)
	}
}

// When every composed type is unknown the schema still has to serialise as
// something an OpenAPI consumer accepts.
func TestPruneAllUnknownAllOfBecomesObject(t *testing.T) {
	doc := buildWith(t, map[string]*core.Schema{
		"T": {AllOf: []*core.Schema{{Ref: refPrefix + "Gone"}}},
	}, "T")

	got := doc.Components.Schemas["T"]
	if got.AllOf != nil {
		t.Errorf("allOf = %+v, want nil", got.AllOf)
	}
	if got.Type != "object" {
		t.Errorf("type = %q, want object", got.Type)
	}
}

// A property naming an unknown type degrades to an untyped object rather than
// pointing at nothing.
func TestPruneDanglingPropertyRef(t *testing.T) {
	doc := buildWith(t, map[string]*core.Schema{
		"T": {Type: "object", Properties: map[string]*core.Schema{
			"missing": {Ref: refPrefix + "Nope"},
			"ok":      {Type: "string"},
		}},
	}, "T")

	p := doc.Components.Schemas["T"].Properties["missing"]
	if p.Ref != "" {
		t.Errorf("ref = %q, want cleared", p.Ref)
	}
	if p.Type != "object" {
		t.Errorf("type = %q, want object", p.Type)
	}
	if doc.Components.Schemas["T"].Properties["ok"].Type != "string" {
		t.Error("sibling property was altered")
	}
	assertNoDanglingRefs(t, doc)
}

func TestPruneDanglingArrayItemRef(t *testing.T) {
	doc := buildWith(t, map[string]*core.Schema{
		"T": {Type: "object", Properties: map[string]*core.Schema{
			"list": {Type: "array", Items: &core.Schema{Ref: refPrefix + "Nope"}},
		}},
	}, "T")

	items := doc.Components.Schemas["T"].Properties["list"].Items
	if items.Ref != "" || items.Type != "object" {
		t.Errorf("items = %+v, want a bare object", items)
	}
	assertNoDanglingRefs(t, doc)
}

func TestPruneDanglingMapValueRef(t *testing.T) {
	doc := buildWith(t, map[string]*core.Schema{
		"T": {Type: "object", Properties: map[string]*core.Schema{
			"m": {Type: "object", AdditionalProperties: &core.Schema{Ref: refPrefix + "Nope"}},
		}},
	}, "T")

	ap := doc.Components.Schemas["T"].Properties["m"].AdditionalProperties
	if ap.Ref != "" || ap.Type != "object" {
		t.Errorf("additionalProperties = %+v, want a bare object", ap)
	}
	assertNoDanglingRefs(t, doc)
}

// Schemas that reference each other must not send the walk into a loop.
func TestPruneHandlesCycles(t *testing.T) {
	node := &core.Schema{Type: "object", Properties: map[string]*core.Schema{}}
	node.Properties["self"] = &core.Schema{Ref: refPrefix + "Node"}
	node.Properties["gone"] = &core.Schema{Ref: refPrefix + "Nope"}

	done := make(chan *core.Document, 1)
	go func() {
		done <- buildWith(t, map[string]*core.Schema{"Node": node}, "Node")
	}()

	select {
	case doc := <-done:
		if doc.Components.Schemas["Node"].Properties["self"].Ref != refPrefix+"Node" {
			t.Error("self-reference was pruned")
		}
		assertNoDanglingRefs(t, doc)
	case <-time.After(5 * time.Second):
		t.Fatal("prune did not terminate on a cyclic schema")
	}
}

// assertNoDanglingRefs is the invariant the whole pass exists to guarantee:
// every $ref in the finished document resolves to a schema in it.
func assertNoDanglingRefs(t *testing.T, doc *core.Document) {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var walk func(v any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if ref, ok := n["$ref"].(string); ok {
				name := strings.TrimPrefix(ref, refPrefix)
				if _, ok := doc.Components.Schemas[name]; !ok {
					t.Errorf("document contains a $ref to missing schema %q", name)
				}
			}
			for _, sub := range n {
				walk(sub)
			}
		case []any:
			for _, sub := range n {
				walk(sub)
			}
		}
	}
	var tree any
	if err := json.Unmarshal(b, &tree); err != nil {
		t.Fatal(err)
	}
	walk(tree)
}

// ---- operationId ----

func TestOperationIDUsesHandlerName(t *testing.T) {
	if got := operationID(core.Route{Method: "get", Path: "/users", HandlerName: "listUsers"}); got != "listUsers" {
		t.Errorf("= %q, want listUsers", got)
	}
}

// A route registered with a closure or a returned handler has no name, so one
// is derived from the method and path rather than left empty.
func TestOperationIDFallsBackToMethodAndPath(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"post", "/graphql", "post_graphql"},
		{"get", "/api/v1/users", "get_api_v1_users"},
		{"get", "/users/{id}", "get_users_ById"},
		{"delete", "/a/b/{userId}", "delete_a_b_ByUserId"},
		{"get", "/", "get"},
		{"get", "/files/{wildcard}", "get_files_ByWildcard"},
		// Characters that cannot appear in an identifier are replaced.
		{"get", "/we-ird.path", "get_we_ird_path"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			got := operationID(core.Route{Method: tc.method, Path: tc.path})
			if got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
		})
	}
}

// Distinct routes must not collapse onto the same id; codegen would emit
// clashing function names.
func TestOperationIDIsUniquePerRoute(t *testing.T) {
	seen := map[string]string{}
	for _, r := range []core.Route{
		{Method: "get", Path: "/users"},
		{Method: "post", Path: "/users"},
		{Method: "get", Path: "/users/{id}"},
		{Method: "get", Path: "/orders"},
	} {
		id := operationID(r)
		if prev, dup := seen[id]; dup {
			t.Errorf("%s %s collides with %s on id %q", r.Method, r.Path, prev, id)
		}
		seen[id] = r.Method + " " + r.Path
	}
}

// The generated document carries the fallback, so no operation is left without
// an operationId.
func TestBuildFillsEveryOperationID(t *testing.T) {
	doc := Build("t", "1", []core.Route{
		{Method: "get", Path: "/named", HandlerName: "listThings"},
		{Method: "post", Path: "/anonymous"},
	}, map[string]*core.Schema{})

	for path, methods := range doc.Paths {
		for method, op := range methods {
			if op.OperationID == "" {
				t.Errorf("%s %s has no operationId", method, path)
			}
		}
	}
	if doc.Paths["/anonymous"]["post"].OperationID != "post_anonymous" {
		t.Errorf("= %q", doc.Paths["/anonymous"]["post"].OperationID)
	}
}

func TestUpperFirst(t *testing.T) {
	cases := map[string]string{"id": "Id", "": "", "a": "A", "Ünicode": "Ünicode", "ünicode": "Ünicode"}
	for in, want := range cases {
		if got := upperFirst(in); got != want {
			t.Errorf("upperFirst(%q) = %q, want %q", in, got, want)
		}
	}
}
