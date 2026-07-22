package core

import (
	"go/parser"
	"go/token"
	"testing"
)

const embeddedSrc = `package sample

type Audit struct {
	CreatedAt string ` + "`json:\"createdAt\"`" + `
	UpdatedAt string ` + "`json:\"updatedAt\"`" + `
}

type Meta struct {
	Tag string ` + "`json:\"tag\"`" + `
}

type User struct {
	Audit
	Meta ` + "`json:\"meta\"`" + `
	Name string ` + "`json:\"name\"`" + `
	skip string
}
`

func TestEmbeddedStructs(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", embeddedSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStructScanner()
	s.Collect(file)

	user := s.Schemas["User"]
	if user == nil {
		t.Fatal("User not collected")
	}

	// Anonymous embed (Audit) is promoted via allOf $ref.
	if len(user.AllOf) != 1 || user.AllOf[0].Ref != "#/components/schemas/Audit" {
		t.Errorf("allOf = %+v, want one $ref to Audit", user.AllOf)
	}
	// Embed with a json tag (Meta) becomes a named property, not promoted.
	if user.Properties["meta"] == nil || user.Properties["meta"].Ref != "#/components/schemas/Meta" {
		t.Errorf("meta property = %+v", user.Properties["meta"])
	}
	if user.Properties["name"] == nil {
		t.Error("name property missing")
	}
	// Unexported fields are never serialized.
	if _, ok := user.Properties["skip"]; ok {
		t.Error("unexported field should be skipped")
	}
}
