package core

import (
	"go/ast"
	"go/token"
	"testing"
)

// ---- schema.go: type-switch arms the parser rarely produces ----

// A file containing a func decl exercises the "not a GenDecl" arm of the
// second pass in Collect.
func TestCollectSkipsFuncDecls(t *testing.T) {
	schemas := scanSrc(t, "package p\n\nfunc F() {}\n\ntype T struct{ A string }\n")
	if schemas["T"] == nil {
		t.Fatal("T not collected alongside a func decl")
	}
}

// The parser only ever puts TypeSpecs in a TYPE decl and ValueSpecs in a
// CONST decl, but Collect guards against hand-built ASTs. Feed it one.
func TestCollectToleratesMalformedGenDecls(t *testing.T) {
	file := &ast.File{
		Name: ast.NewIdent("p"),
		Decls: []ast.Decl{
			&ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{
				&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("x")}},
			}},
			&ast.GenDecl{Tok: token.CONST, Specs: []ast.Spec{
				&ast.TypeSpec{Name: ast.NewIdent("y"), Type: ast.NewIdent("int")},
			}},
		},
	}
	sc := NewStructScanner()
	sc.Collect(file) // must not panic
	if len(sc.Schemas) != 0 {
		t.Errorf("unexpected schemas: %v", sc.Schemas)
	}
}

// A const whose type is a selector expression (time.Duration) is not an enum
// base; the block must reset the current enum type.
func TestEnumConstWithSelectorTypeIsIgnored(t *testing.T) {
	schemas := scanSrc(t, `package p

type Status string

const (
	A Status = "a"
	B time.Duration = 5
)
`)
	s := schemas["Status"]
	if s == nil {
		t.Fatal("no Status schema")
	}
	if len(s.Enum) != 1 || s.Enum[0] != "a" {
		t.Errorf("enum = %v, want [a]", s.Enum)
	}
}

// iota combined with an unresolvable identifier cannot be reduced.
func TestEvalIntUnresolvableOperand(t *testing.T) {
	schemas := scanSrc(t, `package p

type E int

const A E = iota + unknownConst
`)
	if s := schemas["E"]; s != nil && len(s.Enum) != 0 {
		t.Errorf("enum = %v, want empty", s.Enum)
	}
}

// SHR is one of the supported iota operators.
func TestEvalIntShiftRight(t *testing.T) {
	schemas := scanSrc(t, `package p

type E int

const (
	A E = 8 >> iota
	B
)
`)
	s := schemas["E"]
	if s == nil {
		t.Fatal("no E schema")
	}
	if len(s.Enum) != 2 || s.Enum[0] != 8 || s.Enum[1] != 4 {
		t.Errorf("enum = %v, want [8 4]", s.Enum)
	}
}

// litValue's integer arm and its fallthrough-to-nil are exercised directly:
// evalConst only routes string kinds here, so the arm is defensive.
func TestLitValueDirect(t *testing.T) {
	intLit := &ast.BasicLit{Kind: token.INT, Value: "42"}
	if v := litValue(intLit, "integer"); v != 42 {
		t.Errorf("integer lit = %v, want 42", v)
	}
	if v := litValue(intLit, "string"); v != nil {
		t.Errorf("string kind on INT lit = %v, want nil", v)
	}
	badInt := &ast.BasicLit{Kind: token.INT, Value: "not-a-number"}
	if v := litValue(badInt, "integer"); v != nil {
		t.Errorf("unparsable int = %v, want nil", v)
	}
}

// A string-kind enum whose const value is an int literal cannot be decoded.
func TestStringEnumWithIntLiteralDropped(t *testing.T) {
	schemas := scanSrc(t, `package p

type Status string

const A Status = 5
`)
	if s := schemas["Status"]; s != nil && len(s.Enum) != 0 {
		t.Errorf("enum = %v, want empty", s.Enum)
	}
}

// ---- embedded fields ----

// An embedded field with no tag at all is promoted via allOf.
func TestEmbeddedFieldWithoutTag(t *testing.T) {
	schemas := scanSrc(t, `package p

type Base struct {
	ID int `+"`json:\"id\"`"+`
}

type T struct {
	Base
}
`)
	s := schemas["T"]
	if s == nil {
		t.Fatal("no T schema")
	}
	if len(s.AllOf) != 1 || s.AllOf[0].Ref != refPrefix+"Base" {
		t.Errorf("allOf = %+v, want ref to Base", s.AllOf)
	}
}

// An embedded field with a tag that has no json key is still promoted.
func TestEmbeddedFieldWithNonJSONTag(t *testing.T) {
	schemas := scanSrc(t, `package p

type Base struct {
	ID int `+"`json:\"id\"`"+`
}

type T struct {
	Base `+"`xml:\"base\"`"+`
}
`)
	s := schemas["T"]
	if s == nil {
		t.Fatal("no T schema")
	}
	if len(s.AllOf) != 1 || s.AllOf[0].Ref != refPrefix+"Base" {
		t.Errorf("allOf = %+v, want ref to Base", s.AllOf)
	}
}

// An embedded field whose json tag has options keeps only the name.
func TestEmbeddedJSONNameStripsOptions(t *testing.T) {
	schemas := scanSrc(t, `package p

type Base struct {
	ID int `+"`json:\"id\"`"+`
}

type T struct {
	Base `+"`json:\"base,omitempty\"`"+`
}
`)
	s := schemas["T"]
	if s == nil {
		t.Fatal("no T schema")
	}
	if s.Properties["base"] == nil {
		t.Errorf("properties = %v, want base", keysOf(s.Properties))
	}
}

// An embedded basic type is not a named schema; embeddedRef returns nothing.
func TestEmbeddedBasicTypeIgnored(t *testing.T) {
	schemas := scanSrc(t, `package p

type T struct {
	string
	Name string `+"`json:\"name\"`"+`
}
`)
	s := schemas["T"]
	if s == nil {
		t.Fatal("no T schema")
	}
	if len(s.AllOf) != 0 {
		t.Errorf("allOf = %+v, want none", s.AllOf)
	}
	if s.Properties["name"] == nil {
		t.Errorf("properties = %v, want name", keysOf(s.Properties))
	}
}

// ---- validate.go ----

// Both doc helpers guard a nil schema; call them directly.
func TestApplyDocTagsAndValidationNilSchema(t *testing.T) {
	applyDocTags(nil, `doc:"x"`) // must not panic
	if applyValidation(nil, `binding:"required"`) {
		t.Error("nil schema reported required")
	}
}

func TestDocAndExampleTags(t *testing.T) {
	s := schemaFor(t, wrap("\tAge int `json:\"age\" doc:\"age in years\" example:\"25\"`"))
	p := prop(t, s, "age")
	if p.Description != "age in years" {
		t.Errorf("description = %q", p.Description)
	}
	if p.Example != 25 {
		t.Errorf("example = %v (%T), want int 25", p.Example, p.Example)
	}
}

func TestOmitemptyIsNotAConstraint(t *testing.T) {
	s := schemaFor(t, wrap("\tName string `json:\"name\" binding:\"omitempty,min=2\"`"))
	p := prop(t, s, "name")
	if p.MinLength == nil || *p.MinLength != 2 {
		t.Errorf("minLength = %v, want 2", p.MinLength)
	}
	if contains(s.Required, "name") {
		t.Error("omitempty made the field required")
	}
}

// A tag missing its closing quote yields no value.
func TestTagValueUnterminated(t *testing.T) {
	if v := tagValue(`doc:"unterminated`, "doc"); v != "" {
		t.Errorf("got %q, want empty", v)
	}
}

// min/max on a map counts entries; that is not modelled, so it is dropped.
func TestBoundOnMapIsDropped(t *testing.T) {
	s := schemaFor(t, wrap("\tM map[string]string `json:\"m\" binding:\"min=2\"`"))
	p := prop(t, s, "m")
	if p.MinItems != nil || p.MinLength != nil || p.Minimum != nil {
		t.Errorf("bound leaked onto map schema: %+v", p)
	}
}

// len on a number means equality.
func TestLenOnNumberIsEquality(t *testing.T) {
	s := schemaFor(t, wrap("\tN int `json:\"n\" binding:\"len=5\"`"))
	p := prop(t, s, "n")
	if p.Minimum == nil || *p.Minimum != 5 || p.Maximum == nil || *p.Maximum != 5 {
		t.Errorf("min/max = %v/%v, want 5/5", p.Minimum, p.Maximum)
	}
}

// oneof on a float field parses its values as floats.
func TestOneOfNumberTyped(t *testing.T) {
	s := schemaFor(t, wrap("\tF float64 `json:\"f\" binding:\"oneof=1.5 2.5\"`"))
	p := prop(t, s, "f")
	if len(p.Enum) != 2 || p.Enum[0] != 1.5 || p.Enum[1] != 2.5 {
		t.Errorf("enum = %v, want [1.5 2.5]", p.Enum)
	}
}

// A non-numeric value in a numeric oneof discards the whole enum.
func TestOneOfNumberRejectsNonNumeric(t *testing.T) {
	s := schemaFor(t, wrap("\tF float64 `json:\"f\" binding:\"oneof=1.5 nope\"`"))
	p := prop(t, s, "f")
	if len(p.Enum) != 0 {
		t.Errorf("enum = %v, want empty", p.Enum)
	}
}
