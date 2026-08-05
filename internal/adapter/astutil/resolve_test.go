package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// parseFile parses a single Go source string into an *ast.File.
func parseFile(t *testing.T, src string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

func TestStringConsts(t *testing.T) {
	src := `package p
const A = "/a"
const (
	B = "/b"
	C = B + "/c"
)
var D = "/d"
var E = A + "/e"
var F = someCall()
const G = 42
`
	f := parseFile(t, src)
	consts := StringConsts([]*ast.File{f})
	want := map[string]string{"A": "/a", "B": "/b", "C": "/b/c", "D": "/d", "E": "/a/e"}
	for k, v := range want {
		if consts[k] != v {
			t.Errorf("consts[%q] = %q, want %q", k, consts[k], v)
		}
	}
	if _, ok := consts["F"]; ok {
		t.Errorf("F has a dynamic initializer and must be omitted")
	}
	if _, ok := consts["G"]; ok {
		t.Errorf("G is not a string and must be omitted")
	}
}

func TestResolveString(t *testing.T) {
	consts := map[string]string{"base": "/api/v1", "u": "/users"}
	f := parseFile(t, `package p
var _ = ""+base+u+"/x"
`)
	// Extract the RHS expression of the var.
	var expr ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok && len(vs.Values) == 1 {
			expr = vs.Values[0]
		}
		return true
	})
	got, ok := ResolveString(expr, consts)
	if !ok || got != "/api/v1/users/x" {
		t.Fatalf("ResolveString = %q,%v; want /api/v1/users/x,true", got, ok)
	}
}

// A bare string literal must resolve identically to StringLit — this is the
// drop-in guarantee that keeps existing route output byte-identical.
func TestResolveStringLiteralDropIn(t *testing.T) {
	f := parseFile(t, `package p
var _ = "/lit"
`)
	var expr ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok && len(vs.Values) == 1 {
			expr = vs.Values[0]
		}
		return true
	})
	got, ok := ResolveString(expr, nil)
	if !ok || got != "/lit" {
		t.Fatalf("ResolveString(lit,nil) = %q,%v; want /lit,true", got, ok)
	}
}

func TestResolveStringUnresolved(t *testing.T) {
	f := parseFile(t, `package p
func g(paths []string){ _ = paths[0] }
`)
	var expr ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		if ix, ok := n.(*ast.IndexExpr); ok {
			expr = ix
		}
		return true
	})
	if _, ok := ResolveString(expr, map[string]string{}); ok {
		t.Fatalf("index expression must not resolve")
	}
}
