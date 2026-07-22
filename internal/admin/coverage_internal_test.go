package admin

import (
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

// A package name that is not an identifier makes the generated runtime.go
// unparseable, and Generate must refuse to emit it.
func TestGenerateBadPackageFailsGofmt(t *testing.T) {
	d := doc([2]string{"get", "/users"})
	if _, err := Generate(d, Options{Package: "1 not an identifier"}); err == nil {
		t.Fatal("Generate accepted a package name that cannot compile")
	}
}

// An import path containing a quote breaks the generated entrypoint's import
// clause; the error must surface rather than write broken source.
func TestGenerateBadImportPathFailsGofmt(t *testing.T) {
	d := doc([2]string{"get", "/users"})
	if _, err := Generate(d, Options{ImportPath: `example.com/x"`, Dir: "admin"}); err == nil {
		t.Fatal("Generate accepted an import path that cannot compile")
	}
}

// An empty Dir defaults to "admin" in the entrypoint's usage comment.
func TestGenerateEntrypointDefaultsDir(t *testing.T) {
	d := doc([2]string{"get", "/users"})
	files, err := Generate(d, Options{ImportPath: "example.com/x/admin"})
	if err != nil {
		t.Fatal(err)
	}
	var entry []byte
	for _, f := range files {
		if f.Name == "cmd/adminpanel/main.go" {
			entry = f.Data
		}
	}
	if entry == nil {
		t.Fatal("no entrypoint written despite ImportPath")
	}
	if !strings.Contains(string(entry), "./admin/cmd/adminpanel") {
		t.Errorf("usage comment does not use the default dir:\n%s", entry)
	}
}

func TestRenderMissingTemplate(t *testing.T) {
	if _, err := render("does-not-exist.tmpl", nil); err == nil {
		t.Fatal("render found a template that does not exist")
	}
}

func TestRenderExecuteError(t *testing.T) {
	// main.go.tmpl dereferences .ImportPath, which an int does not have.
	if _, err := render("main.go.tmpl", 42); err == nil {
		t.Fatal("render executed a template against data missing its fields")
	}
}

func TestGofmtRejectsInvalidSource(t *testing.T) {
	if _, err := gofmt("x.go", []byte("this is not go")); err == nil {
		t.Fatal("gofmt accepted invalid source")
	}
}

func TestWriteActionOptionalFields(t *testing.T) {
	var b strings.Builder
	writeAction(&b, "List", &Action{
		Method: "GET", Path: "/users", Label: "List users", Handler: "listUsers",
		Summary: "All users.", Secured: true, Headers: []string{"X-Sign"},
	})
	out := b.String()
	for _, want := range []string{
		`Summary: "All users."`, "Secured: true", `Headers: []string{"X-Sign"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %s:\n%s", want, out)
		}
	}
}

func TestItemSuffix(t *testing.T) {
	if _, _, ok := itemSuffix("/users/abc"); ok {
		t.Error("a literal segment was mistaken for a path parameter")
	}
	if _, _, ok := itemSuffix("abc"); ok {
		t.Error("a path with no slash was accepted")
	}
	if c, p, ok := itemSuffix("/users/{id}"); !ok || c != "/users" || p != "id" {
		t.Errorf("= %q, %q, %v", c, p, ok)
	}
}

func TestBuildTakesBaseURLFromServers(t *testing.T) {
	d := doc([2]string{"get", "/users"})
	d.Servers = []core.Server{{URL: "https://api.example.com"}}
	if m := Build(d); m.BaseURL != "https://api.example.com" {
		t.Errorf("BaseURL = %q", m.BaseURL)
	}
}

func TestStripVerbNoMatch(t *testing.T) {
	if got := stripVerb("update", "get", "show"); got != "" {
		t.Errorf("= %q, want empty", got)
	}
	// A name that is exactly the verb has nothing left to strip.
	if got := stripVerb("get", "get"); got != "" {
		t.Errorf("= %q, want empty", got)
	}
}

func TestDerefGivesUpOnCycles(t *testing.T) {
	d := core.NewDocument("t", "1")
	d.Components.Schemas["A"] = &core.Schema{Ref: "#/components/schemas/A"}
	if got := deref(d, d.Components.Schemas["A"]); got == nil || got.Ref == "" {
		t.Errorf("cycle did not stop with the ref intact: %+v", got)
	}
}

func TestSchemaType(t *testing.T) {
	d := core.NewDocument("t", "1")
	d.Components.Schemas["Money"] = &core.Schema{Type: "object"}

	cases := []struct {
		name string
		s    *core.Schema
		want string
	}{
		{"nil", nil, ""},
		{"stated", &core.Schema{Type: "integer"}, "integer"},
		{"ref resolved", &core.Schema{Ref: "#/components/schemas/Money"}, "object"},
		{"ref dangling", &core.Schema{Ref: "#/components/schemas/Nope"}, "object"},
		{"properties", &core.Schema{Properties: map[string]*core.Schema{"a": nil}}, "object"},
		{"allOf", &core.Schema{AllOf: []*core.Schema{{}}}, "object"},
		{"empty", &core.Schema{}, ""},
	}
	for _, c := range cases {
		if got := schemaType(d, c.s); got != c.want {
			t.Errorf("%s: = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFieldsOfHonoursLimit(t *testing.T) {
	d := core.NewDocument("t", "1")
	props := map[string]*core.Schema{}
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		props[n] = &core.Schema{Type: "string"}
	}
	if got := fieldsOf(d, &core.Schema{Properties: props}, 8); len(got) != 8 {
		t.Errorf("len = %d, want 8", len(got))
	}
}

func TestTitleizeEmpty(t *testing.T) {
	if got := titleize(""); got != "" {
		t.Errorf("= %q", got)
	}
}
