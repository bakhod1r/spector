package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func parseFunc(t *testing.T, body string) *ast.FuncDecl {
	t.Helper()
	src := "package p\nfunc mw() {\n" + body + "\n}\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f.Decls[0].(*ast.FuncDecl)
}

func TestAnalyzeNilFunc(t *testing.T) {
	if h, s := analyze(nil); h != nil || s != nil {
		t.Errorf("= %v, %v, want nil, nil", h, s)
	}
	if h, s := analyze(&ast.FuncDecl{Name: ast.NewIdent("x")}); h != nil || s != nil {
		t.Errorf("bodyless: = %v, %v, want nil, nil", h, s)
	}
}

// A non-call statement before a return is not a WriteHeader refusal.
func TestReturnedRejectionsSkipsNonCallStatements(t *testing.T) {
	fd := parseFunc(t, "<-ch\nreturn")
	if _, statuses := analyze(fd); len(statuses) != 0 {
		t.Errorf("statuses = %v, want none", statuses)
	}
}

func TestIntLitRejectsNonIntLiteral(t *testing.T) {
	if _, ok := intLit(expr(t, `"forbidden"`)); ok {
		t.Error("a string literal was read as a status code")
	}
}

func TestAuthFromHeadersAuthorization(t *testing.T) {
	scheme, def, ok := authFromHeaders([]string{"X-Request-Id", "Authorization"})
	if !ok || scheme != "bearerAuth" {
		t.Fatalf("= %q, %v", scheme, ok)
	}
	if def.Type != "http" || def.Scheme != "bearer" {
		t.Errorf("scheme = %+v", def)
	}
}

func TestNilIndexIsInert(t *testing.T) {
	var ix *Index
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", "package p\nfunc A() {}\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	ix.Collect(f) // must not panic
	if got := ix.For("r", token.NoPos, nil); got != nil {
		t.Errorf("For on nil index = %v", got)
	}
}

// An empty receiver name has nothing to inherit from.
func TestForEmptyReceiver(t *testing.T) {
	ix := NewIndex()
	if got := ix.For("", token.NoPos, nil); got != nil {
		t.Errorf("= %v, want nil chain", got)
	}
}
