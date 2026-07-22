package graphqlsdl

import (
	"testing"

	"github.com/vektah/gqlparser/v2/ast"
)

// Built-in GraphQL scalars map to JSON Schema types; anything else is a
// reference to a type defined in the schema.
func TestTypeSchemaScalars(t *testing.T) {
	cases := []struct {
		named string
		typ   string
		ref   string
	}{
		{named: "String", typ: "string"},
		{named: "ID", typ: "string"},
		{named: "Int", typ: "integer"},
		{named: "Float", typ: "number"},
		{named: "Boolean", typ: "boolean"},
		{named: "User", ref: "#/components/schemas/User"},
		{named: "OrderStatus", ref: "#/components/schemas/OrderStatus"},
	}
	for _, tc := range cases {
		t.Run(tc.named, func(t *testing.T) {
			got := typeSchema(&ast.Type{NamedType: tc.named})
			if got.Type != tc.typ {
				t.Errorf("type = %q, want %q", got.Type, tc.typ)
			}
			if got.Ref != tc.ref {
				t.Errorf("ref = %q, want %q", got.Ref, tc.ref)
			}
		})
	}
}

func TestTypeSchemaList(t *testing.T) {
	got := typeSchema(&ast.Type{Elem: &ast.Type{NamedType: "String"}})
	if got.Type != "array" {
		t.Fatalf("type = %q, want array", got.Type)
	}
	if got.Items == nil || got.Items.Type != "string" {
		t.Errorf("items = %+v, want string", got.Items)
	}
}

func TestTypeSchemaNestedList(t *testing.T) {
	got := typeSchema(&ast.Type{Elem: &ast.Type{Elem: &ast.Type{NamedType: "Int"}}})
	if got.Type != "array" || got.Items == nil || got.Items.Type != "array" {
		t.Fatalf("outer = %+v", got)
	}
	if got.Items.Items == nil || got.Items.Items.Type != "integer" {
		t.Errorf("innermost = %+v, want integer", got.Items.Items)
	}
}

func TestTypeSchemaListOfNamedType(t *testing.T) {
	got := typeSchema(&ast.Type{Elem: &ast.Type{NamedType: "User"}})
	if got.Items == nil || got.Items.Ref != "#/components/schemas/User" {
		t.Errorf("items = %+v, want a $ref to User", got.Items)
	}
}

// Non-null is carried by NonNull on the type, which does not change the
// mapping: a list stays a list, a scalar stays a scalar.
func TestTypeSchemaNonNullIgnoredForShape(t *testing.T) {
	got := typeSchema(&ast.Type{NamedType: "String", NonNull: true})
	if got.Type != "string" {
		t.Errorf("type = %q, want string", got.Type)
	}
}
