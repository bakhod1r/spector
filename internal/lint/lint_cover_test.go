package lint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

func parseFunc(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "x.go", "package p\n"+src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			return fd
		}
	}
	t.Fatal("no function in the snippet")
	return nil
}

func TestLooksLikeHandlerShapes(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		// Unnamed parameters count the same as named ones.
		{"func h(*gin.Context) {}", true},
		{"func h(c echo.Context) error { return nil }", true},
		{"func h(c echo.Context) string { return \"\" }", false},
		{"func h(w http.ResponseWriter, r *http.Request) {}", true},
		{"func h(w http.ResponseWriter, r *http.Request) error { return nil }", false},
		{"func h(w http.ResponseWriter, r *http.Request) (a, b error) { return }", false},
	}
	for _, tc := range cases {
		if got := looksLikeHandler(parseFunc(t, tc.src)); got != tc.want {
			t.Errorf("looksLikeHandler(%q) = %v, want %v", tc.src, got, tc.want)
		}
	}
}

// A method's receiver list exists but a nil parameter list means no handler.
func TestLooksLikeHandlerNilParams(t *testing.T) {
	fd := &ast.FuncDecl{Name: ast.NewIdent("h"), Type: &ast.FuncType{}}
	if looksLikeHandler(fd) {
		t.Error("a function with no parameter list is not a handler")
	}
}

// A deeply qualified type has no single package identifier to render.
func TestTypeStringDeepSelector(t *testing.T) {
	deep := &ast.SelectorExpr{
		X:   &ast.SelectorExpr{X: ast.NewIdent("a"), Sel: ast.NewIdent("b")},
		Sel: ast.NewIdent("C"),
	}
	if got := typeString(deep); got != "C" {
		t.Errorf("typeString = %q, want C", got)
	}
	if got := typeString(&ast.ArrayType{Elt: ast.NewIdent("byte")}); got != "" {
		t.Errorf("typeString on an array = %q, want empty", got)
	}
}

// Two registrations under different handler names must name both locations.
func TestDuplicateWithDifferentHandlersNamesBoth(t *testing.T) {
	got := duplicates([]core.Route{
		{Method: "get", Path: "/x", HandlerName: "a", Source: &core.Source{File: "a.go", Line: 1}},
		{Method: "get", Path: "/x", HandlerName: "b", Source: &core.Source{File: "b.go", Line: 2}},
	})
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	msg := got[0].Message
	if !strings.Contains(msg, "a at a.go:1") || !strings.Contains(msg, "b at b.go:2") {
		t.Errorf("message does not cite both locations: %s", msg)
	}
}

// Two findings of the same kind sort by message.
func TestSameKindSortsByMessage(t *testing.T) {
	fs, err := Analyze("testdata/app", []core.Route{
		{Method: "get", Path: "/b", HandlerName: "h1"},
		{Method: "get", Path: "/b", HandlerName: "h1"},
		{Method: "get", Path: "/a", HandlerName: "h2"},
		{Method: "get", Path: "/a", HandlerName: "h2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	dups := ofKind(fs, DuplicateRoute)
	if len(dups) != 2 {
		t.Fatalf("got %d duplicate findings, want 2: %v", len(dups), fs)
	}
	if dups[0].Message > dups[1].Message {
		t.Errorf("not sorted by message: %v", dups)
	}
}
