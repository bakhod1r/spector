package sdk

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

// goType and tsType are the two type mappers; every schema shape the document
// model can hold must map to something, including the shapes the happy-path
// document never contains (nil, number, boolean, maps, free-form objects,
// enums, and types the generator does not know).
func TestTypeMappers(t *testing.T) {
	cases := []struct {
		name   string
		schema *core.Schema
		wantGo string
		wantTS string
	}{
		{"nil is the widest type", nil, "any", "unknown"},
		{"ref resolves to the named type", &core.Schema{Ref: "#/components/schemas/User"}, "User", "User"},
		{"string", &core.Schema{Type: "string"}, "string", "string"},
		{"integer", &core.Schema{Type: "integer"}, "int", "number"},
		{"number", &core.Schema{Type: "number"}, "float64", "number"},
		{"boolean", &core.Schema{Type: "boolean"}, "bool", "boolean"},
		{
			"array of scalars",
			&core.Schema{Type: "array", Items: &core.Schema{Type: "string"}},
			"[]string", "string[]",
		},
		{
			"array with no items is an array of the widest type",
			&core.Schema{Type: "array"},
			"[]any", "unknown[]",
		},
		{
			"object with additionalProperties is a map",
			&core.Schema{Type: "object", AdditionalProperties: &core.Schema{Type: "integer"}},
			"map[string]int", "Record<string, number>",
		},
		{
			"object with no properties at all is free-form",
			&core.Schema{Type: "object"},
			"map[string]any", "Record<string, unknown>",
		},
		{
			"inline object with properties: Go widens, TS keeps the shape",
			&core.Schema{Type: "object", Properties: map[string]*core.Schema{
				"b": {Type: "string"},
				"a": {Type: "integer"},
			}},
			"map[string]any", "{ a: number; b: string }",
		},
		{
			"unknown type falls back",
			&core.Schema{Type: "geometry"},
			"any", "unknown",
		},
		{
			"string enum becomes a TS union but stays a Go string",
			&core.Schema{Type: "string", Enum: []any{"a", "b"}},
			"string", `"a" | "b"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := goType(tc.schema); got != tc.wantGo {
				t.Errorf("goType = %q, want %q", got, tc.wantGo)
			}
			if got := tsType(tc.schema); got != tc.wantTS {
				t.Errorf("tsType = %q, want %q", got, tc.wantTS)
			}
		})
	}
}

// Parameter names come from the document and may not be legal identifiers, or
// may collide with the locals the generated method declares.
func TestParamNames(t *testing.T) {
	cases := []struct {
		in     string
		wantGo string
		wantTS string
	}{
		{"id", "id", "id"},
		{"user-id", "userId", "userId"},
		{"", "param", "param"},    // nothing survives sanitising
		{"---", "param", "param"}, // ditto: only separators
		{"ctx", "ctxParam", "ctx"},
		{"body", "bodyParam", "body"},
		{"query", "queryParam", "query"},
		{"path", "pathParam", "path"},
		{"out", "outParam", "out"},
		{"err", "errParam", "err"},
		{"c", "cParam", "c"},
	}
	for _, tc := range cases {
		if got := goParamName(tc.in); got != tc.wantGo {
			t.Errorf("goParamName(%q) = %q, want %q", tc.in, got, tc.wantGo)
		}
		if got := tsParamName(tc.in); got != tc.wantTS {
			t.Errorf("tsParamName(%q) = %q, want %q", tc.in, got, tc.wantTS)
		}
	}
}

// A digit ends a word, so the next letter is upper-cased again.
func TestExportNameDigits(t *testing.T) {
	if got := exportName("v1users"); got != "V1Users" {
		t.Errorf("exportName = %q, want %q", got, "V1Users")
	}
}

// TS property names that are not legal identifiers must be quoted.
func TestTSPropName(t *testing.T) {
	cases := map[string]string{
		"name":      "name",
		"_private":  "_private",
		"$ref":      "$ref",
		"user2":     "user2",
		"2fa":       `"2fa"`, // a leading digit is illegal
		"user-id":   `"user-id"`,
		"":          `""`,
		"has space": `"has space"`,
	}
	for in, want := range cases {
		if got := tsPropName(in); got != want {
			t.Errorf("tsPropName(%q) = %q, want %q", in, got, want)
		}
	}
}

// An empty document still yields compiling, non-empty clients: no schemas, no
// operations, and no server means the constructor default is the empty string.
func TestEmptyDocument(t *testing.T) {
	d := core.NewDocument("Empty", "1.0.0")

	goFiles, err := Generate(d, Options{Lang: "golang"}) // also covers the "golang" alias and the default package
	if err != nil {
		t.Fatal(err)
	}
	src := string(goFiles[0].Data)
	if !strings.Contains(src, "package client") {
		t.Error("empty Package should default to client")
	}
	fset := token.NewFileSet()
	if _, perr := parser.ParseFile(fset, "client.go", src, 0); perr != nil {
		t.Fatalf("generated Go does not parse: %v", perr)
	}

	tsFiles, err := Generate(d, Options{Lang: "typescript"}) // covers the "typescript" alias
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tsFiles[0].Data), "export class Client {") {
		t.Error("empty document should still emit a Client class")
	}
}

// A nil operation, a nil named schema, and a nil response are all reachable
// through the model; none of them may panic or emit junk.
func TestNilsAreSkipped(t *testing.T) {
	d := core.NewDocument("Nils", "1.0.0")
	d.Components.Schemas["Ghost"] = nil
	d.Paths["/x"] = map[string]*core.Operation{"get": nil}
	d.AddOperation("/y", "get", &core.Operation{
		Responses: map[string]*core.Response{
			"200": nil,                                                    // present but empty
			"404": {},                                                     // not a success
			"201": {Content: map[string]core.MediaType{"text/plain": {}}}, // success, wrong media type
		},
	})

	for _, lang := range []string{"go", "ts"} {
		files, err := Generate(d, Options{Lang: lang})
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if strings.Contains(string(files[0].Data), "Ghost") {
			t.Errorf("%s: a nil schema should emit nothing", lang)
		}
	}

	if ops := flatten(d); len(ops) != 1 {
		t.Fatalf("nil operation should be skipped: got %d ops", len(ops))
	} else if ops[0].Result != nil {
		t.Error("no JSON success response, so Result should be nil")
	}
}

// Non-object and property-less named schemas become aliases rather than
// structs/interfaces.
func TestNamedSchemaAliases(t *testing.T) {
	d := core.NewDocument("Aliases", "1.0.0")
	d.Components.Schemas["Status"] = &core.Schema{Type: "string", Enum: []any{"on", "off"}}
	d.Components.Schemas["Bag"] = &core.Schema{Type: "object"} // object, but no properties

	goFiles, err := Generate(d, Options{Lang: "go"})
	if err != nil {
		t.Fatal(err)
	}
	goSrc := string(goFiles[0].Data)
	for _, want := range []string{"type Status = string", "type Bag = map[string]any"} {
		if !strings.Contains(goSrc, want) {
			t.Errorf("client.go missing %q", want)
		}
	}

	tsFiles, err := Generate(d, Options{Lang: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	tsSrc := string(tsFiles[0].Data)
	for _, want := range []string{`export type Status = "on" | "off";`, "export type Bag = Record<string, unknown>;"} {
		if !strings.Contains(tsSrc, want) {
			t.Errorf("client.ts missing %q", want)
		}
	}
}

// Two operations can derive — or declare — the same name; a numeric suffix
// keeps the output compiling instead of redeclaring an identifier.
func TestDuplicateNamesGetSuffixed(t *testing.T) {
	d := core.NewDocument("Dupes", "1.0.0")
	d.AddOperation("/a", "get", &core.Operation{OperationID: "fetch"})
	d.AddOperation("/b", "get", &core.Operation{OperationID: "fetch"})

	ops := flatten(d)
	if len(ops) != 2 || ops[0].Name != "fetch" || ops[1].Name != "fetch2" {
		t.Fatalf("names = %q, %q", ops[0].Name, ops[1].Name)
	}

	files, err := Generate(d, Options{Lang: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, perr := parser.ParseFile(token.NewFileSet(), "client.go", files[0].Data, 0); perr != nil {
		t.Fatalf("generated Go does not parse: %v", perr)
	}
}

// Summaries and deprecation flags become doc comments in both languages.
func TestSummaryAndDeprecated(t *testing.T) {
	d := core.NewDocument("Docs", "1.0.0")
	d.AddOperation("/a", "get", &core.Operation{
		OperationID: "getA",
		Summary:     "fetches an A.",
		Deprecated:  true,
	})
	// Deprecated with no summary exercises the other half of the TS branch.
	d.AddOperation("/b", "get", &core.Operation{OperationID: "getB", Deprecated: true})

	goFiles, err := Generate(d, Options{Lang: "go"})
	if err != nil {
		t.Fatal(err)
	}
	goSrc := string(goFiles[0].Data)
	if !strings.Contains(goSrc, "// GetA fetches an A.") {
		t.Error("summary missing from client.go")
	}
	if !strings.Contains(goSrc, "// Deprecated: the API marks this operation deprecated.") {
		t.Error("deprecation missing from client.go")
	}

	tsFiles, err := Generate(d, Options{Lang: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	tsSrc := string(tsFiles[0].Data)
	for _, want := range []string{"   * fetches an A.", "   * @deprecated"} {
		if !strings.Contains(tsSrc, want) {
			t.Errorf("client.ts missing %q", want)
		}
	}
}

// A path parameter with no usable characters still needs a legal argument, and
// the placeholder substitution must use the same name the signature declares.
func TestOddParamNameRoundTrips(t *testing.T) {
	d := core.NewDocument("Odd", "1.0.0")
	d.AddOperation("/things/{--}", "get", &core.Operation{
		OperationID: "getThing",
		Parameters:  []core.Parameter{{Name: "--", In: "path", Required: true}},
	})

	goFiles, err := Generate(d, Options{Lang: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goFiles[0].Data), "param string") {
		t.Error("unusable path parameter name should fall back to param")
	}
	if _, perr := parser.ParseFile(token.NewFileSet(), "client.go", goFiles[0].Data, 0); perr != nil {
		t.Fatalf("generated Go does not parse: %v", perr)
	}

	tsFiles, err := Generate(d, Options{Lang: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tsFiles[0].Data), "param: string | number") {
		t.Error("unusable path parameter name should fall back to param in TS too")
	}
}

// An explicit BaseURL overrides the document's first server.
func TestExplicitBaseURLWins(t *testing.T) {
	files, err := Generate(doc(), Options{Lang: "ts", BaseURL: "https://api.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files[0].Data), `"https://api.example.com"`) {
		t.Error("explicit BaseURL not used")
	}
}
