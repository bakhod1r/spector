package calls

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/user/specter/internal/core"
)

func TestNilIndexCollectIsANoOp(t *testing.T) {
	var ix *Index
	ix.Collect(&ast.File{}) // must not panic
}

// The named import is parsed into the import map; the call itself goes through
// a field, so it is matched by the receiver-name convention.
func TestNamedImportResolves(t *testing.T) {
	got := analyze(t, "namedImport")
	if !has(got, core.CallCache, "cache.Get") {
		t.Errorf("= %v, want the guessed cache call", targets(got))
	}
	for _, c := range got {
		if c.Confidence != core.Likely {
			t.Errorf("%s: confidence = %q, want likely", c.Target, c.Confidence)
		}
	}
}

func TestClientFieldIsAGuessedHTTPCall(t *testing.T) {
	got := analyze(t, "callsOut")
	if !has(got, core.CallHTTP, "client.Do") {
		t.Errorf("= %v, want the guessed http call", targets(got))
	}
}

// Plumbing, unresolvable receivers, unknown helpers, and chains deeper than
// maxDepth all classify as nothing.
func TestOddShapesReportNothing(t *testing.T) {
	if got := analyze(t, "oddShapes"); len(got) != 0 {
		t.Errorf("= %v, want no calls", targets(got))
	}
}

func TestSameKindSortsByTarget(t *testing.T) {
	got := analyze(t, "twoQueries")
	if len(got) != 2 || got[0].Target > got[1].Target {
		t.Errorf("= %v, want two db calls sorted by target", targets(got))
	}
}

// A declaration the index never collected has no file, so no imports.
func TestImportsOfUncollectedFuncIsNil(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", "package p\nfunc f() { g() }\nfunc g() {}", 0)
	if err != nil {
		t.Fatal(err)
	}
	fd := file.Decls[0].(*ast.FuncDecl)
	ix := NewIndex()
	if imports := ix.importsOf(fd); imports != nil {
		t.Errorf("= %v, want nil", imports)
	}
	if got := Analyze(fd, ix); len(got) != 0 {
		t.Errorf("= %v, want nothing", targets(got))
	}
}

func TestMethodFitsUnknownKind(t *testing.T) {
	if methodFits("teleport", "Do") {
		t.Error("an unknown kind must fit no method")
	}
}
