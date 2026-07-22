package calls

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/user/specter/internal/core"
)

// load parses the fixture package and returns an Index over it.
func load(t *testing.T) *Index {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, "testdata/app", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex()
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ix.Collect(file)
		}
	}
	return ix
}

func analyze(t *testing.T, name string) []core.Call {
	t.Helper()
	ix := load(t)
	fd := ix.funcs[name]
	if fd == nil {
		t.Fatalf("no func %q in the fixture", name)
	}
	return Analyze(fd, ix)
}

// targets renders the result as "kind target" strings for easy comparison.
func targets(cs []core.Call) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Kind+" "+c.Target)
	}
	return out
}

func has(cs []core.Call, kind, substr string) bool {
	for _, c := range cs {
		if c.Kind == kind && strings.Contains(c.Target, substr) {
			return true
		}
	}
	return false
}

func TestDirectDatabaseCall(t *testing.T) {
	got := analyze(t, "getUser")
	if !has(got, core.CallDB, "QueryRowContext") {
		t.Errorf("= %v, want a db call", targets(got))
	}
}

// The dependency is two levels below the handler. Stopping at the handler body
// would miss it, and almost every real service has this shape.
func TestFollowsDelegationChain(t *testing.T) {
	got := analyze(t, "listOrders")
	if !has(got, core.CallDB, "QueryContext") {
		t.Errorf("= %v, want the db call found through loadOrders -> queryOrders",
			targets(got))
	}
}

func TestReportsEveryKind(t *testing.T) {
	got := analyze(t, "checkout")
	for _, want := range []struct{ kind, target string }{
		{core.CallDB, "ExecContext"},
		{core.CallCache, "Set"},
		{core.CallQueue, "WriteMessages"},
		{core.CallHTTP, "Get"},
	} {
		if !has(got, want.kind, want.target) {
			t.Errorf("missing %s %s in %v", want.kind, want.target, targets(got))
		}
	}
}

// A call through an imported package is proven by the import statement; one
// matched on a receiver's name is a convention. Presenting a guess as a fact is
// the failure mode this whole feature has to avoid.
func TestConfidenceReflectsHowItWasFound(t *testing.T) {
	for _, c := range analyze(t, "checkout") {
		switch {
		case strings.HasPrefix(c.Target, "http."):
			if c.Confidence != core.Certain {
				t.Errorf("%s: confidence = %q, want certain (it came from an import)",
					c.Target, c.Confidence)
			}
		case strings.HasPrefix(c.Target, "s."):
			// Receiver-based matches are guesses whatever the receiver is.
			if c.Confidence != core.Likely {
				t.Errorf("%s: confidence = %q, want likely", c.Target, c.Confidence)
			}
		}
	}
}

// A handler that reaches nothing must say so. Listing every method call would
// bury the few that matter.
func TestQuietHandlerReportsNothing(t *testing.T) {
	if got := analyze(t, "ping"); len(got) != 0 {
		t.Errorf("= %v, want no calls", targets(got))
	}
}

// A map index is not a cache read, and a local named cache is not a dependency.
func TestDoesNotInventCallsFromNames(t *testing.T) {
	if got := analyze(t, "traps"); len(got) != 0 {
		t.Errorf("= %v, want no calls", targets(got))
	}
}

// A recursive handler must terminate. Without the seen set this walks forever.
func TestRecursionTerminates(t *testing.T) {
	done := make(chan []core.Call, 1)
	go func() { done <- analyze(t, "recurse") }()
	select {
	case got := <-done:
		if !has(got, core.CallDB, "Query") {
			t.Errorf("= %v, want the db call still found", targets(got))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Analyze did not terminate on a recursive function")
	}
}

// The document has to be reproducible; map iteration is not.
func TestResultIsSorted(t *testing.T) {
	first := targets(analyze(t, "checkout"))
	for i := 0; i < 5; i++ {
		if got := targets(analyze(t, "checkout")); !equal(got, first) {
			t.Fatalf("run %d differs:\n %v\n %v", i, got, first)
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i-1] > first[i] {
			t.Errorf("not sorted: %v", first)
			break
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNilInputs(t *testing.T) {
	if got := Analyze(nil, load(t)); got != nil {
		t.Errorf("nil decl gave %v", targets(got))
	}
	if got := Analyze(&ast.FuncDecl{Name: ast.NewIdent("x")}, load(t)); got != nil {
		t.Errorf("bodyless decl gave %v", targets(got))
	}
	if got := Analyze(load(t).funcs["getUser"], nil); got != nil {
		t.Errorf("nil index gave %v", targets(got))
	}
}

// Two scans must not see each other's declarations. A package-level index
// would leak between them and, in a server, grow without bound.
func TestIndexesAreIndependent(t *testing.T) {
	a, b := load(t), NewIndex()
	if len(a.funcs) == 0 {
		t.Fatal("fixture index is empty")
	}
	if len(b.funcs) != 0 {
		t.Errorf("a fresh index already has %d funcs", len(b.funcs))
	}
}
