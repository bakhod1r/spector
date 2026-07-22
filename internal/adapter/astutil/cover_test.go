package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// parseDoc parses a file with one commented function and returns its doc.
func parseDoc(t *testing.T, doc string) *ast.CommentGroup {
	t.Helper()
	src := "package p\n" + doc + "func F() {}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return file.Decls[0].(*ast.FuncDecl).Doc
}

// ---- DocComment ----

func TestDocCommentStripsDirectiveLines(t *testing.T) {
	doc := parseDoc(t, "// F does things.\n// specter:tags a,b\n// More detail.\n")
	summary, desc := DocComment(doc, "F")
	if summary != "does things." {
		t.Errorf("summary = %q", summary)
	}
	if desc != "More detail." {
		t.Errorf("description = %q, directive line should be stripped", desc)
	}
}

// ---- SourceOf ----

func TestSourceOfEmptyFilename(t *testing.T) {
	fset := token.NewFileSet()
	f := fset.AddFile("", -1, 100)
	if s := SourceOf(fset, ".", f.Pos(1)); s != nil {
		t.Errorf("SourceOf with empty filename = %+v, want nil", s)
	}
}

// ---- LocalTypesWith ----

func TestLocalTypesDeclStmtNonGenDecl(t *testing.T) {
	// The parser never puts a FuncDecl inside a DeclStmt, but the type allows
	// it; the walk must skip it rather than panic.
	body := &ast.BlockStmt{List: []ast.Stmt{
		&ast.DeclStmt{Decl: &ast.FuncDecl{Name: ast.NewIdent("g"), Type: &ast.FuncType{}}},
	}}
	if got := LocalTypes(body); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestLocalTypesMultiValueRhs(t *testing.T) {
	// Two expressions on the right: not the f() shape, resolve nothing.
	if got := LocalTypes(parseBody(t, "a, b := 1, 2")); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestLocalTypesNonIdentLhs(t *testing.T) {
	if got := LocalTypes(parseBody(t, "s.x = User{}")); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestLocalTypesSingleAssignFromCall(t *testing.T) {
	returns := map[string]TypeInfo{"listUsers": {Name: "User", Array: true}}
	got := LocalTypesWith(parseBody(t, "u := listUsers()"), returns)
	if got["u"].Name != "User" || !got["u"].Array {
		t.Errorf("u = %+v, want []User via returns index", got["u"])
	}
}

func TestLocalTypesMultiAssignFromCall(t *testing.T) {
	returns := map[string]TypeInfo{"listUsers": {Name: "User"}}
	got := LocalTypesWith(parseBody(t, "out, err := listUsers()"), returns)
	if got["out"].Name != "User" {
		t.Errorf("out = %+v, want User", got["out"])
	}
	if _, ok := got["err"]; ok {
		t.Errorf("err resolved to a payload type: %v", got)
	}
}

func TestLocalTypesMultiAssignSkipsBlankAndErr(t *testing.T) {
	returns := map[string]TypeInfo{"f": {Name: "User"}}
	got := LocalTypesWith(parseBody(t, "_, out := f()"), returns)
	if got["out"].Name != "User" {
		t.Errorf("out = %+v, blank lhs should be skipped", got["out"])
	}
	got = LocalTypesWith(parseBody(t, "err, out := f()"), returns)
	if got["out"].Name != "User" {
		t.Errorf("out = %+v, err lhs should be skipped", got["out"])
	}
}

func TestLocalTypesMultiAssignNonIdentLhs(t *testing.T) {
	returns := map[string]TypeInfo{"f": {Name: "User"}}
	got := LocalTypesWith(parseBody(t, "s.a, out = f()"), returns)
	if got["out"].Name != "User" {
		t.Errorf("out = %+v, non-ident lhs should be skipped", got["out"])
	}
}

// ---- callType ----

func TestCallTypeMethodOnReceiver(t *testing.T) {
	returns := map[string]TypeInfo{"db.listUsers": {Name: "User"}, "listUsers": {Name: "Wrong"}}
	got := LocalTypesWith(parseBody(t, "u := db.listUsers()"), returns)
	if got["u"].Name != "User" {
		t.Errorf("u = %+v, want the recv-qualified entry to win", got["u"])
	}
}

func TestCallTypeMethodFallbackToBareName(t *testing.T) {
	returns := map[string]TypeInfo{"other": {Name: "Thing"}}
	got := LocalTypesWith(parseBody(t, "x := db.other()"), returns)
	if got["x"].Name != "Thing" {
		t.Errorf("x = %+v, want bare-name fallback", got["x"])
	}
}

func TestCallTypeUnresolvableFun(t *testing.T) {
	returns := map[string]TypeInfo{"f": {Name: "User"}}
	// call.Fun is itself a call: neither ident nor selector.
	got := LocalTypesWith(parseBody(t, "x := g()()"), returns)
	if _, ok := got["x"]; ok {
		t.Errorf("x resolved from an unresolvable fun: %v", got)
	}
}

// ---- primaryResponse ----

func TestPrimaryResponseFallsBackToFirstTyped(t *testing.T) {
	got := primaryResponse([]Response{
		{Status: 404, Type: TypeInfo{}},
		{Status: 400, Type: TypeInfo{Name: "Err"}},
	}, TypeInfo{})
	if got.Name != "Err" {
		t.Errorf("got %+v, want the first typed non-2xx response", got)
	}
}
