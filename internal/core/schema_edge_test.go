package core

import "testing"

// ---- iota arithmetic in enum consts ----

func TestEnumIotaExpressions(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []any
	}{
		{
			name: "bare iota",
			src: `package p
type K int
const (
	A K = iota
	B
	C
)`,
			want: []any{0.0, 1.0, 2.0},
		},
		{
			name: "iota plus offset",
			src: `package p
type K int
const (
	A K = iota + 1
	B
)`,
			want: []any{1.0, 2.0},
		},
		{
			name: "offset minus iota",
			src: `package p
type K int
const (
	A K = 10 - iota
	B
)`,
			want: []any{10.0, 9.0},
		},
		{
			name: "shift left",
			src: `package p
type K int
const (
	A K = 1 << iota
	B
	C
)`,
			want: []any{1.0, 2.0, 4.0},
		},
		{
			name: "parenthesised",
			src: `package p
type K int
const (
	A K = (iota + 2)
	B
)`,
			want: []any{2.0, 3.0},
		},
		{
			name: "multiplication",
			src: `package p
type K int
const (
	A K = iota * 3
	B
	C
)`,
			want: []any{0.0, 3.0, 6.0},
		},
		{
			name: "explicit integer literals",
			src: `package p
type K int
const (
	A K = 5
	B K = 7
)`,
			want: []any{5.0, 7.0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schemas := scanSrc(t, tc.src)
			k := schemas["K"]
			if k == nil {
				t.Fatalf("enum type K not collected; got %v", keys(schemas))
			}
			if k.Type != "integer" {
				t.Errorf("type = %q, want integer", k.Type)
			}
			if len(k.Enum) != len(tc.want) {
				t.Fatalf("enum = %v, want %v", k.Enum, tc.want)
			}
			for i, w := range tc.want {
				if toFloat(k.Enum[i]) != w {
					t.Errorf("enum[%d] = %v, want %v", i, k.Enum[i], w)
				}
			}
		})
	}
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	}
	return -1
}

func TestEnumStringValues(t *testing.T) {
	schemas := scanSrc(t, `package p
type Status string
const (
	Active   Status = "active"
	Disabled Status = "disabled"
)`)
	s := schemas["Status"]
	if s == nil {
		t.Fatalf("Status not collected; got %v", keys(schemas))
	}
	if s.Type != "string" {
		t.Errorf("type = %q, want string", s.Type)
	}
	if len(s.Enum) != 2 || s.Enum[0] != "active" || s.Enum[1] != "disabled" {
		t.Errorf("enum = %v", s.Enum)
	}
}

// A named type over an unsupported base is not an enum we can describe.
func TestEnumUnsupportedBaseIgnored(t *testing.T) {
	schemas := scanSrc(t, `package p
type Ratio float64
const (
	Half Ratio = 0.5
)`)
	if s, ok := schemas["Ratio"]; ok && len(s.Enum) > 0 {
		t.Errorf("float-backed type produced an enum: %v", s.Enum)
	}
}

// A const expression that cannot be reduced must drop that value rather than
// emit a wrong one.
func TestEnumUnreducibleValueSkipped(t *testing.T) {
	schemas := scanSrc(t, `package p
type K int
const (
	A K = someFunc()
)`)
	if s, ok := schemas["K"]; ok && len(s.Enum) > 0 {
		t.Errorf("unreducible const produced enum %v", s.Enum)
	}
}

// A string enum whose value is not a plain literal cannot be resolved.
func TestEnumStringNonLiteralSkipped(t *testing.T) {
	schemas := scanSrc(t, `package p
type S string
const (
	A S = other
)`)
	if s, ok := schemas["S"]; ok && len(s.Enum) > 0 {
		t.Errorf("non-literal string const produced enum %v", s.Enum)
	}
}

// Consts of some other type must not leak into the enum.
func TestEnumIgnoresForeignConsts(t *testing.T) {
	schemas := scanSrc(t, `package p
type K int
const (
	A K = 1
)
const Unrelated = 99`)
	k := schemas["K"]
	if k == nil {
		t.Fatal("K not collected")
	}
	if len(k.Enum) != 1 {
		t.Errorf("enum = %v, want only A", k.Enum)
	}
}

// ---- embedded fields ----

// An embedded struct with no json tag is promoted: its properties compose in
// via allOf rather than nesting under a key.
func TestEmbeddedPromotedViaAllOf(t *testing.T) {
	schemas := scanSrc(t, `package p
type Base struct{ ID int }
type T struct {
	Base
	Name string
}`)
	tt := schemas["T"]
	if len(tt.AllOf) != 1 || tt.AllOf[0].Ref != "#/components/schemas/Base" {
		t.Errorf("allOf = %+v, want a $ref to Base", tt.AllOf)
	}
	if _, ok := tt.Properties["Name"]; !ok {
		t.Errorf("properties = %v, want Name alongside allOf", keys(tt.Properties))
	}
}

// With an explicit json tag the embedded type nests under that key instead.
func TestEmbeddedWithJSONTagNests(t *testing.T) {
	schemas := scanSrc(t, `package p
type Base struct{ ID int }
type T struct {
	Base `+"`json:\"base\"`"+`
}`)
	tt := schemas["T"]
	if len(tt.AllOf) != 0 {
		t.Errorf("allOf = %+v, want none for a tagged embed", tt.AllOf)
	}
	if p, ok := tt.Properties["base"]; !ok || p.Ref != "#/components/schemas/Base" {
		t.Errorf("properties = %v, want base -> $ref", keys(tt.Properties))
	}
}

// A struct made only of promoted embeds carries no empty properties map.
func TestOnlyEmbeddedDropsPropertiesMap(t *testing.T) {
	schemas := scanSrc(t, `package p
type Base struct{ ID int }
type T struct{ Base }`)
	tt := schemas["T"]
	if tt.Properties != nil {
		t.Errorf("properties = %v, want nil when only allOf is present", keys(tt.Properties))
	}
	if tt.Type != "" {
		t.Errorf("type = %q, want empty alongside allOf", tt.Type)
	}
}

func TestEmbeddedPointerPromoted(t *testing.T) {
	schemas := scanSrc(t, `package p
type Base struct{ ID int }
type T struct{ *Base }`)
	tt := schemas["T"]
	if len(tt.AllOf) != 1 || tt.AllOf[0].Ref != "#/components/schemas/Base" {
		t.Errorf("allOf = %+v, want a $ref to Base for an embedded pointer", tt.AllOf)
	}
}

// An embedded pkg.T is referenced by its bare type name. That is what makes
// cross-package embeds inside a project resolve, since the scanner collects
// every package's structs into one flat namespace.
//
// Embedding a type the scan never sees (sync.Mutex, gorm.Model) therefore
// produces a $ref that may not resolve. That is expected at this layer: the
// full set of names is not known until collection finishes, so gen.Build
// prunes whatever is still unresolved before the document is emitted.
func TestEmbeddedQualifiedTypeRefsByBareName(t *testing.T) {
	schemas := scanSrc(t, `package p
type T struct{ sync.Mutex }`)
	tt := schemas["T"]
	if len(tt.AllOf) != 1 || tt.AllOf[0].Ref != "#/components/schemas/Mutex" {
		t.Errorf("allOf = %+v, want a $ref by bare name", tt.AllOf)
	}
}

// An embedded field tagged json:"-" is neither promoted nor nested.
func TestEmbeddedDashSkipped(t *testing.T) {
	schemas := scanSrc(t, `package p
type Base struct{ ID int }
type T struct {
	Base `+"`json:\"-\"`"+`
	Name string
}`)
	tt := schemas["T"]
	if _, ok := tt.Properties["-"]; ok {
		t.Error(`embedded field tagged json:"-" was documented`)
	}
}

// ---- json tag parsing edge cases ----

func TestTagWithoutJSONKeyUsesGoName(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype T struct {\n Name string `db:\"name_col\"`\n}\n")
	if _, ok := schemas["T"].Properties["Name"]; !ok {
		t.Errorf("properties = %v, want the Go name", keys(schemas["T"].Properties))
	}
}

func TestTagWithEmptyJSONNameUsesGoName(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype T struct {\n Name string `json:\",omitempty\"`\n}\n")
	if _, ok := schemas["T"].Properties["Name"]; !ok {
		t.Errorf("properties = %v, want the Go name", keys(schemas["T"].Properties))
	}
}

func TestMalformedTagUsesGoName(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype T struct {\n Name string `json:\"unterminated`\n}\n")
	if _, ok := schemas["T"].Properties["Name"]; !ok {
		t.Errorf("properties = %v, want the Go name for a malformed tag", keys(schemas["T"].Properties))
	}
}

func TestMultipleNamesShareATag(t *testing.T) {
	// Both identifiers are serialized; the tag cannot disambiguate them, which
	// mirrors what encoding/json itself does with such a declaration.
	schemas := scanSrc(t, "package p\ntype T struct {\n A, B string\n C int\n}\n")
	props := schemas["T"].Properties
	for _, want := range []string{"A", "B", "C"} {
		if _, ok := props[want]; !ok {
			t.Errorf("properties = %v, want %s", keys(props), want)
		}
	}
}

func TestMixedExportedAndUnexportedInOneDecl(t *testing.T) {
	schemas := scanSrc(t, "package p\ntype T struct {\n A, b string\n}\n")
	props := schemas["T"].Properties
	if _, ok := props["b"]; ok {
		t.Error("unexported identifier in a multi-name field was documented")
	}
	if _, ok := props["A"]; !ok {
		t.Errorf("properties = %v, want A", keys(props))
	}
}
