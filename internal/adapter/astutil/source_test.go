package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// parseInDir writes src as a file in a temp dir and returns its AST plus a
// Locator rooted at that dir, mirroring how an adapter is set up.
func parseInDir(t *testing.T, name, src string) (*ast.File, Locator) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return file, Locator{Fset: fset, Dir: dir}
}

const srcTwoFuncs = `package p

func first() {}

func second() {}
`

func funcDecl(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
			return fd
		}
	}
	t.Fatalf("no func %q", name)
	return nil
}

// The path must be relative to the scanned directory: an absolute one differs
// per machine and would leak the developer's home directory into a spec that
// is meant to be committed.
func TestSourceOfIsRelativeToScanDir(t *testing.T) {
	file, loc := parseInDir(t, "handlers.go", srcTwoFuncs)

	got := loc.Of(funcDecl(t, file, "first").Pos())
	if got == nil {
		t.Fatal("nil source for a valid position")
	}
	if got.File != "handlers.go" {
		t.Errorf("File = %q, want handlers.go", got.File)
	}
	if filepath.IsAbs(got.File) {
		t.Errorf("File is absolute: %q", got.File)
	}
	if got.Line != 3 {
		t.Errorf("Line = %d, want 3", got.Line)
	}
}

func TestSourceOfReportsDistinctLines(t *testing.T) {
	file, loc := parseInDir(t, "h.go", srcTwoFuncs)
	first, second := loc.Of(funcDecl(t, file, "first").Pos()), loc.Of(funcDecl(t, file, "second").Pos())
	if first.Line == second.Line {
		t.Fatalf("both funcs report line %d", first.Line)
	}
	if second.Line != 5 {
		t.Errorf("second = %d, want 5", second.Line)
	}
}

// nil rather than a half-filled value, so a caller can never mistake a zero
// line for line 1.
func TestSourceOfInvalidInputsAreNil(t *testing.T) {
	file, loc := parseInDir(t, "h.go", srcTwoFuncs)
	pos := funcDecl(t, file, "first").Pos()

	if got := loc.Of(token.NoPos); got != nil {
		t.Errorf("invalid position gave %+v, want nil", got)
	}
	if got := (Locator{}).Of(pos); got != nil {
		t.Errorf("zero Locator gave %+v, want nil", got)
	}
	if got := SourceOf(nil, "", pos); got != nil {
		t.Errorf("nil fset gave %+v, want nil", got)
	}
}

// A file outside the scanned directory keeps its own path: a relative path
// climbing out of the tree would be less useful than an honest absolute one.
func TestSourceOfOutsideScanDirKeepsPath(t *testing.T) {
	file, loc := parseInDir(t, "h.go", srcTwoFuncs)
	loc.Dir = filepath.Join(t.TempDir(), "elsewhere")

	got := loc.Of(funcDecl(t, file, "first").Pos())
	if got == nil {
		t.Fatal("nil source")
	}
	if got.File == "" || got.File[0] == '.' {
		t.Errorf("File = %q, want the original path rather than a ../ climb", got.File)
	}
}

// ---- Locator.Handler ----

// The declaration is what a reader wants to open, so it wins over the line that
// registered the route.
func TestHandlerPrefersTheDeclaration(t *testing.T) {
	file, loc := parseInDir(t, "h.go", srcTwoFuncs)
	fd := funcDecl(t, file, "second")

	got := loc.Handler(fd, funcDecl(t, file, "first"))
	if got.Line != 5 {
		t.Errorf("Line = %d, want the declaration's line 5", got.Line)
	}
}

// A route registered with a closure has no declaration; the registration line
// is still better than nothing.
func TestHandlerFallsBackToTheCallSite(t *testing.T) {
	file, loc := parseInDir(t, "h.go", srcTwoFuncs)

	got := loc.Handler(nil, funcDecl(t, file, "second"))
	if got == nil {
		t.Fatal("nil source, want the call site")
	}
	if got.Line != 5 {
		t.Errorf("Line = %d, want 5", got.Line)
	}
}

func TestHandlerWithNothingToPointAt(t *testing.T) {
	_, loc := parseInDir(t, "h.go", srcTwoFuncs)
	if got := loc.Handler(nil, nil); got != nil {
		t.Errorf("= %+v, want nil", got)
	}
}
