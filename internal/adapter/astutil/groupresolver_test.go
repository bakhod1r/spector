package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func parseGroupSrc(t *testing.T, src string) (*token.FileSet, []*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return fset, []*ast.File{f}
}

// Two groups assigned from each other would walk forever without the guard.
// The source is nonsense, but a scanner must not hang on nonsense.
func TestGroupResolverCycle(t *testing.T) {
	fset, files := parseGroupSrc(t, `package main
func main() {
	a := b.Group("/a")
	b := a.Group("/b")
	_, _ = a, b
}`)
	var diags Diagnostics
	g := NewGroupResolver(NewScope(fset, files), files, NewResolver(files), &diags, Locator{Fset: fset})
	fn := files[0].Decls[0].(*ast.FuncDecl)
	if got := g.Prefix("a", fn); got != "/b/a" {
		t.Errorf("cycle: got %q, want /b/a", got)
	}
	if got := g.Prefix("missing", fn); got != "" {
		t.Errorf("missing: got %q, want empty", got)
	}
}

// A nil resolver answers rather than panicking: adapters build one lazily and
// a scan of a tree with no groups at all must still work.
func TestGroupResolverNil(t *testing.T) {
	var g *GroupResolver
	if got := g.Prefix("r", nil); got != "" {
		t.Errorf("nil resolver: got %q", got)
	}
}

// The same name in two functions is two different routers. A table keyed by
// name alone gave one function's routes the other's prefix.
func TestGroupResolverScopesNamesPerFunction(t *testing.T) {
	fset, files := parseGroupSrc(t, `package main
func users(r Router) {
	group := r.Group("/users")
	_ = group
}
func orders(r Router) {
	group := r.Group("/orders")
	_ = group
}`)
	var diags Diagnostics
	g := NewGroupResolver(NewScope(fset, files), files, NewResolver(files), &diags, Locator{Fset: fset})
	decls := map[string]*ast.FuncDecl{}
	for _, d := range files[0].Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			decls[fd.Name.Name] = fd
		}
	}
	if got := g.Prefix("group", decls["users"]); got != "/users" {
		t.Errorf("users group = %q, want /users", got)
	}
	if got := g.Prefix("group", decls["orders"]); got != "/orders" {
		t.Errorf("orders group = %q, want /orders", got)
	}
}
