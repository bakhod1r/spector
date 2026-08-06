package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func parseSrc(t *testing.T, src string) []*ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return []*ast.File{f}
}

// firstArg returns the first argument of the first call to fnName in files.
func firstArg(t *testing.T, files []*ast.File, fnName string) ast.Expr {
	t.Helper()
	var found ast.Expr
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if found != nil {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == fnName && len(call.Args) > 0 {
				found = call.Args[0]
			}
			return true
		})
	}
	if found == nil {
		t.Fatalf("no call to %s found", fnName)
	}
	return found
}

func mustResolve(t *testing.T, src, fn, want string) {
	t.Helper()
	files := parseSrc(t, src)
	got, ok := NewResolver(files).Resolve(firstArg(t, files, fn))
	if !ok || got != want {
		t.Fatalf("Resolve = (%q,%v), want (%q,true)", got, ok, want)
	}
}

func mustMask(t *testing.T, src, fn string) {
	t.Helper()
	files := parseSrc(t, src)
	if got, ok := NewResolver(files).Resolve(firstArg(t, files, fn)); ok {
		t.Fatalf("Resolve = (%q,true), want masked (_,false)", got)
	}
}

func TestResolverLocalPrefix(t *testing.T) {
	mustResolve(t, `package p
func routes() { base := "/v1"; use(base+"/x") }
func use(string) {}`, "use", "/v1/x")
}

func TestResolverMultiHop(t *testing.T) {
	mustResolve(t, `package p
func routes() { base := "/v1"; sub := base+"/api"; use(sub+"/x") }
func use(string) {}`, "use", "/v1/api/x")
}

func TestResolverLocalShadowsPackageConst(t *testing.T) {
	mustResolve(t, `package p
const base = "/admin"
func routes() { base := "/v1"; use(base+"/x") }
func use(string) {}`, "use", "/v1/x")
}

func TestResolverMaskedShadowNoFallback(t *testing.T) {
	// The bug fix: a local shadow that is unresolvable must NOT resolve to the
	// package const of the same name.
	mustMask(t, `package p
const base = "/admin"
func routes() { base := ext(); use(base+"/x") }
func ext() string { return "" }
func use(string) {}`, "use")
}

func TestResolverReassignDiffers(t *testing.T) {
	mustMask(t, `package p
func routes(c bool) { base := "/v1"; if c { base = "/v2" }; use(base+"/x") }
func use(string) {}`, "use")
}

func TestResolverDynamicSources(t *testing.T) {
	mustMask(t, `package p
func routes(p string) { use(p+"/x") }
func use(string) {}`, "use")
	mustMask(t, `package p
func routes(m map[string]string) { base := m["k"]; use(base) }
func use(string) {}`, "use")
	mustMask(t, `package p
func routes(paths []string) { for _, base := range paths { use(base) } }
func use(string) {}`, "use")
}

func TestResolverNestedFuncLit(t *testing.T) {
	mustResolve(t, `package p
func routes() {
	base := "/v1"
	run(func() { sub := base+"/api"; use(sub+"/x") })
}
func run(func()) {}
func use(string) {}`, "use", "/v1/api/x")
}

func TestResolverNoScopeDropIn(t *testing.T) {
	// A bare literal outside any function resolves like StringLit.
	files := parseSrc(t, `package p
var x = "/lit"`)
	// Grab the literal directly.
	var lit ast.Expr
	ast.Inspect(files[0], func(n ast.Node) bool {
		if b, ok := n.(*ast.BasicLit); ok && b.Kind == token.STRING {
			lit = b
		}
		return true
	})
	if got, ok := NewResolver(files).Resolve(lit); !ok || got != "/lit" {
		t.Fatalf("Resolve(lit) = (%q,%v), want (/lit,true)", got, ok)
	}
}

func TestResolverPackageConstStillWorks(t *testing.T) {
	mustResolve(t, `package p
const base = "/v1"
func routes() { use(base+"/x") }
func use(string) {}`, "use", "/v1/x")
}
