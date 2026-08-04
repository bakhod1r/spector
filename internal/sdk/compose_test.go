package sdk

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

// composedDoc mirrors what the scanner emits for an embedded struct: the
// composing type carries an allOf $ref to the embedded one plus its own
// properties. This is the shape a real Go API produces whenever a handler
// returns a struct that embeds another — audit fields, base models, and so on.
func composedDoc() *core.Document {
	d := core.NewDocument("Composed", "1")
	d.Servers = []core.Server{{URL: "http://x"}}

	d.Components.Schemas["Audit"] = &core.Schema{
		Type:     "object",
		Required: []string{"createdAt"},
		Properties: map[string]*core.Schema{
			"createdAt": {Type: "string"},
			"createdBy": {Type: "string"},
		},
	}
	d.Components.Schemas["User"] = &core.Schema{
		// Anonymous embed of Audit, promoted via allOf, plus the type's own
		// fields. No Type/Properties of its own beyond what allOf and Properties
		// carry — exactly what internal/core emits.
		AllOf: []*core.Schema{{Ref: "#/components/schemas/Audit"}},
		Properties: map[string]*core.Schema{
			"id":   {Type: "integer"},
			"name": {Type: "string"},
		},
		Required: []string{"id"},
	}

	get := &core.Operation{
		Responses: map[string]*core.Response{
			"200": {Description: "ok", Content: map[string]core.MediaType{
				"application/json": {Schema: &core.Schema{Ref: "#/components/schemas/User"}},
			}},
		},
	}
	d.AddOperation("/users/{id}", "get", get)
	return d
}

// A composed type must carry the embedded type's fields. Dropping them —
// which is what ignoring allOf does — produces a client that silently cannot
// see createdAt, and a caller who trusts the types never learns the field
// exists.
func TestGoComposedTypeCarriesEmbeddedFields(t *testing.T) {
	files, err := Generate(composedDoc(), Options{Lang: "go", Package: "c"})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files[0].Data)

	fset := token.NewFileSet()
	if _, perr := parser.ParseFile(fset, "client.go", src, 0); perr != nil {
		t.Fatalf("generated Go does not parse:\n%v\n%s", perr, src)
	}

	// Embedding by name is how Go composes, and it gives the caller Audit's
	// fields on a User value.
	if !strings.Contains(src, "type User struct {") {
		t.Fatalf("User is not a struct:\n%s", src)
	}
	userDecl := section(src, "type User struct {")
	if !strings.Contains(userDecl, "\n\tAudit\n") {
		t.Errorf("User does not embed Audit:\n%s", userDecl)
	}
	// Collapse whitespace so gofmt's column alignment (Id   int) still matches.
	if !strings.Contains(strings.Join(strings.Fields(userDecl), " "), "Id int") {
		t.Errorf("User is missing its own id field:\n%s", userDecl)
	}
}

func TestTSComposedTypeExtendsTheEmbeddedOne(t *testing.T) {
	files, err := Generate(composedDoc(), Options{Lang: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files[0].Data)

	// extends gives a User the Audit members without restating them.
	if !strings.Contains(src, "export interface User extends Audit {") {
		t.Errorf("User does not extend Audit:\n%s", section(src, "export interface User"))
	}
	if !strings.Contains(src, "id: number") {
		t.Errorf("User is missing its own id field:\n%s", src)
	}
}

// A named schema that is nothing but an allOf of one ref is an alias, and must
// still name the type it composes rather than collapsing to any/unknown.
func TestPureAliasThroughAllOf(t *testing.T) {
	d := core.NewDocument("t", "1")
	d.Components.Schemas["Base"] = &core.Schema{Type: "object", Properties: map[string]*core.Schema{"x": {Type: "string"}}}
	d.Components.Schemas["Alias"] = &core.Schema{AllOf: []*core.Schema{{Ref: "#/components/schemas/Base"}}}
	d.AddOperation("/a", "get", &core.Operation{Responses: map[string]*core.Response{
		"200": {Description: "ok", Content: map[string]core.MediaType{
			"application/json": {Schema: &core.Schema{Ref: "#/components/schemas/Alias"}},
		}},
	}})

	goSrc := mustGen(t, d, Options{Lang: "go", Package: "c"})
	if !strings.Contains(goSrc, "type Alias = Base") {
		t.Errorf("Go alias not emitted:\n%s", section(goSrc, "type Alias"))
	}
	tsSrc := mustGen(t, d, Options{Lang: "ts"})
	if !strings.Contains(tsSrc, "export type Alias = Base") && !strings.Contains(tsSrc, "export interface Alias extends Base") {
		t.Errorf("TS alias not emitted:\n%s", tsSrc)
	}
}

func mustGen(t *testing.T, d *core.Document, opts Options) string {
	t.Helper()
	files, err := Generate(d, opts)
	if err != nil {
		t.Fatal(err)
	}
	return string(files[0].Data)
}

// section returns the text from marker to the next blank line, so an assertion
// about one declaration is not satisfied by another.
func section(src, marker string) string {
	i := strings.Index(src, marker)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest, "\n}\n"); j >= 0 {
		return rest[:j+3]
	}
	return rest
}
