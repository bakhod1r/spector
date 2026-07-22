package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// scanSrc parses a Go source snippet and returns the schemas collected from it.
func scanSrc(t *testing.T, src string) map[string]*Schema {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	sc := NewStructScanner()
	sc.Collect(file)
	return sc.Schemas
}

// fieldSchema parses a single struct field's type expression.
func fieldSchema(t *testing.T, goType string) *Schema {
	t.Helper()
	schemas := scanSrc(t, "package p\ntype T struct {\n F "+goType+" `json:\"f\"`\n}\n")
	got := schemas["T"]
	if got == nil {
		t.Fatalf("type T was not collected for field type %q", goType)
	}
	return got.Properties["f"]
}

func TestBasicFieldTypes(t *testing.T) {
	cases := []struct {
		goType     string
		wantType   string
		wantFormat string
	}{
		{"string", "string", ""},
		{"bool", "boolean", ""},
		{"int", "integer", ""},
		{"int8", "integer", ""},
		{"int16", "integer", ""},
		{"int32", "integer", ""},
		{"int64", "integer", ""},
		{"uint", "integer", ""},
		{"uint8", "integer", ""},
		{"uint16", "integer", ""},
		{"uint32", "integer", ""},
		{"uint64", "integer", ""},
		{"float32", "number", ""},
		{"float64", "number", ""},
		{"time.Time", "string", "date-time"},
	}
	for _, tc := range cases {
		t.Run(tc.goType, func(t *testing.T) {
			got := fieldSchema(t, tc.goType)
			if got.Type != tc.wantType {
				t.Errorf("type = %q, want %q", got.Type, tc.wantType)
			}
			if got.Format != tc.wantFormat {
				t.Errorf("format = %q, want %q", got.Format, tc.wantFormat)
			}
		})
	}
}

// A pointer documents the same shape as its element: nullability is
// deliberately not modelled.
func TestPointerFieldUnwraps(t *testing.T) {
	got := fieldSchema(t, "*string")
	if got.Type != "string" {
		t.Errorf("type = %q, want string", got.Type)
	}
}

func TestPointerToNamedTypeRefs(t *testing.T) {
	got := fieldSchema(t, "*Address")
	if got.Ref != "#/components/schemas/Address" {
		t.Errorf("ref = %q", got.Ref)
	}
}

func TestSliceField(t *testing.T) {
	got := fieldSchema(t, "[]string")
	if got.Type != "array" {
		t.Fatalf("type = %q, want array", got.Type)
	}
	if got.Items == nil || got.Items.Type != "string" {
		t.Errorf("items = %+v, want string", got.Items)
	}
}

func TestSliceOfPointersToNamedType(t *testing.T) {
	got := fieldSchema(t, "[]*Item")
	if got.Type != "array" {
		t.Fatalf("type = %q, want array", got.Type)
	}
	if got.Items == nil || got.Items.Ref != "#/components/schemas/Item" {
		t.Errorf("items = %+v, want a $ref to Item", got.Items)
	}
}

func TestNestedSlice(t *testing.T) {
	got := fieldSchema(t, "[][]int")
	if got.Type != "array" || got.Items == nil || got.Items.Type != "array" {
		t.Fatalf("outer/inner = %+v", got)
	}
	if got.Items.Items == nil || got.Items.Items.Type != "integer" {
		t.Errorf("innermost = %+v, want integer", got.Items.Items)
	}
}

func TestMapField(t *testing.T) {
	got := fieldSchema(t, "map[string]int")
	if got.Type != "object" {
		t.Fatalf("type = %q, want object", got.Type)
	}
	if got.AdditionalProperties == nil || got.AdditionalProperties.Type != "integer" {
		t.Errorf("additionalProperties = %+v, want integer", got.AdditionalProperties)
	}
}

func TestMapOfNamedType(t *testing.T) {
	got := fieldSchema(t, "map[string]Widget")
	if got.AdditionalProperties == nil || got.AdditionalProperties.Ref != "#/components/schemas/Widget" {
		t.Errorf("additionalProperties = %+v, want a $ref", got.AdditionalProperties)
	}
}

// A named type resolves to a reference so the schema is emitted once and
// shared.
func TestNamedTypeRefs(t *testing.T) {
	got := fieldSchema(t, "Address")
	if got.Ref != "#/components/schemas/Address" {
		t.Errorf("ref = %q", got.Ref)
	}
}

// A qualified type other than time.Time has no known shape, so it degrades to
// a bare object rather than a dangling $ref.
func TestForeignQualifiedTypeIsObject(t *testing.T) {
	got := fieldSchema(t, "uuid.UUID")
	if got.Type != "object" || got.Ref != "" {
		t.Errorf("schema = %+v, want a bare object", got)
	}
}

// Types the mapper has no case for must still produce something valid.
func TestUnknownExprFallsBackToObject(t *testing.T) {
	for _, goType := range []string{"chan int", "func()", "interface{}"} {
		t.Run(goType, func(t *testing.T) {
			got := fieldSchema(t, goType)
			if got == nil || got.Type != "object" {
				t.Errorf("schema = %+v, want a bare object", got)
			}
		})
	}
}

func TestExprToSchemaNilSafe(t *testing.T) {
	// A nil expression must not panic; it is the degenerate case of the
	// default arm.
	var expr ast.Expr
	if got := exprToSchema(expr); got == nil || got.Type != "object" {
		t.Errorf("schema = %+v, want a bare object", got)
	}
}

// ---- json tag handling ----

func TestJSONTagRenamesField(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype T struct {\n Name string `json:\"user_name\"`\n}\n")
	if _, ok := schemas["T"].Properties["user_name"]; !ok {
		t.Errorf("properties = %v, want user_name", keys(schemas["T"].Properties))
	}
}

func TestJSONTagOmitemptyIsStripped(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype T struct {\n Name string `json:\"name,omitempty\"`\n}\n")
	if _, ok := schemas["T"].Properties["name"]; !ok {
		t.Errorf("properties = %v, want name without the option suffix", keys(schemas["T"].Properties))
	}
}

// A field tagged json:"-" is not serialised, so it must not be documented.
func TestJSONTagDashSkipsField(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype T struct {\n Secret string `json:\"-\"`\n Shown string `json:\"shown\"`\n}\n")
	props := schemas["T"].Properties
	if _, ok := props["-"]; ok {
		t.Error(`field tagged json:"-" was documented`)
	}
	if len(props) != 1 {
		t.Errorf("properties = %v, want only shown", keys(props))
	}
}

// Without a tag the Go field name is used as-is.
func TestUntaggedFieldUsesGoName(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype T struct {\n Name string\n}\n")
	if _, ok := schemas["T"].Properties["Name"]; !ok {
		t.Errorf("properties = %v, want Name", keys(schemas["T"].Properties))
	}
}

// Unexported fields never reach JSON, so they must not appear.
func TestUnexportedFieldSkipped(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype T struct {\n hidden string\n Shown string\n}\n")
	if _, ok := schemas["T"].Properties["hidden"]; ok {
		t.Error("unexported field was documented")
	}
}

func TestMultipleNamesOnOneField(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype T struct {\n A, B string\n}\n")
	props := schemas["T"].Properties
	if len(props) != 2 || props["A"] == nil || props["B"] == nil {
		t.Errorf("properties = %v, want both A and B", keys(props))
	}
}

// ---- collection scope ----

func TestNonStructTypesAreNotCollected(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype Alias = int\ntype Fn func()\ntype S struct{}\n")
	if _, ok := schemas["S"]; !ok {
		t.Error("struct S was not collected")
	}
	if _, ok := schemas["Fn"]; ok {
		t.Error("func type was collected as a schema")
	}
}

func TestEmptyFileCollectsNothing(t *testing.T) {
	if got := scanSrc(t, "package p\n"); len(got) != 0 {
		t.Errorf("schemas = %v, want none", keys(got))
	}
}

func TestMultipleStructsCollected(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype A struct{ X int }\ntype B struct{ Y string }\n")
	if len(schemas) != 2 {
		t.Errorf("schemas = %v, want A and B", keys(schemas))
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
