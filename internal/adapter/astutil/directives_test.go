package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func parseFileT(t *testing.T, src string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "t.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParseDirectives(t *testing.T) {
	if d := ParseDirectives(nil); len(d.Tags) != 0 || d.OperationID != "" || d.Deprecated {
		t.Errorf("nil doc: %+v", d)
	}

	f := parseFileT(t, `package p

// listUsers returns users.
// specter:tags users, admin,
// specter:operationId ListUsers
// specter:deprecated
// specter:unknown ignored
func listUsers() {}
`)
	fd := f.Decls[0].(*ast.FuncDecl)
	d := ParseDirectives(fd.Doc)
	if len(d.Tags) != 2 || d.Tags[0] != "users" || d.Tags[1] != "admin" {
		t.Errorf("Tags = %v", d.Tags)
	}
	if d.OperationID != "ListUsers" {
		t.Errorf("OperationID = %q", d.OperationID)
	}
	if !d.Deprecated {
		t.Error("Deprecated not set")
	}
}

func TestReturns(t *testing.T) {
	f := parseFileT(t, `package p

type DB struct{}

func (db *DB) listUsers() ([]User, error) { return nil, nil }
func plain() User { return User{} }
func onlyErr() error { return nil }
func noResults() {}
`)
	out := Returns([]*ast.File{f})
	if info, ok := out["plain"]; !ok || info.Name != "User" {
		t.Errorf("plain = %+v, %v", out["plain"], ok)
	}
	if info, ok := out["listUsers"]; !ok || info.Name != "User" || !info.Array {
		t.Errorf("listUsers = %+v, %v", info, ok)
	}
	if _, ok := out["DB.listUsers"]; !ok {
		t.Error("receiver-qualified key missing")
	}
	if _, ok := out["onlyErr"]; ok {
		t.Error("error-only function indexed")
	}
	if _, ok := out["noResults"]; ok {
		t.Error("resultless function indexed")
	}
}
