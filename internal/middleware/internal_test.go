package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func expr(t *testing.T, src string) ast.Expr {
	t.Helper()
	e, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return e
}

func TestExprName(t *testing.T) {
	cases := map[string]string{
		"AuthRequired":         "AuthRequired",
		"jwt.Auth":             "jwt.Auth",
		"a.b.C":                "C",
		`AuthRequired("x")`:    "AuthRequired",
		"func() {}":            "func literal",
		`"not middleware"`:     "",
	}
	for src, want := range cases {
		if got := exprName(expr(t, src)); got != want {
			t.Errorf("exprName(%s) = %q, want %q", src, got, want)
		}
	}
}

func TestIndexChain(t *testing.T) {
	var nilIx *Index
	if got := nilIx.Chain([]ast.Expr{expr(t, "Auth")}); got != nil {
		t.Errorf("nil index: got %v", got)
	}

	src := `package p
func AuthRequired() {}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex()
	ix.Collect(f)
	// Duplicates collapse to the first mention.
	chain := ix.Chain([]ast.Expr{expr(t, "AuthRequired"), expr(t, "AuthRequired"), expr(t, "Logger")})
	if len(chain) != 2 {
		t.Fatalf("chain = %+v, want 2 entries", chain)
	}
	if chain[0].Name != "AuthRequired" || chain[1].Name != "Logger" {
		t.Errorf("chain = %+v", chain)
	}
}

func TestStringLit(t *testing.T) {
	if s, ok := stringLit(expr(t, `"X-API-Key"`)); !ok || s != "X-API-Key" {
		t.Errorf("got %q, %v", s, ok)
	}
	if _, ok := stringLit(expr(t, `""`)); ok {
		t.Error("empty string accepted")
	}
	if _, ok := stringLit(expr(t, "name")); ok {
		t.Error("non-literal accepted")
	}
	if _, ok := stringLit(&ast.BasicLit{Kind: token.STRING, Value: `"broken`}); ok {
		t.Error("unterminated literal accepted")
	}
}
