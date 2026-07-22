package pbgo

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/user/specter/internal/core"
)

func TestScanMissingDir(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing directory must be an error")
	}
}

func TestScanInvalidGeneratedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.pb.go"), []byte("package p\nfunc {"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(dir); err == nil {
		t.Error("an unparsable .pb.go must be an error")
	}
}

// A referenced message with no struct behind it is simply not collected.
func TestScanUnknownMessageTypes(t *testing.T) {
	dir := t.TempDir()
	src := `package p

import (
	"context"

	"google.golang.org/grpc"
)

type GhostServer interface {
	Get(context.Context, *GhostRequest) (*GhostResponse, error)
}

var Ghost_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "GhostService",
	HandlerType: (*GhostServer)(nil),
}
`
	if err := os.WriteFile(filepath.Join(dir, "ghost_grpc.pb.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Services) != 1 || len(doc.Messages) != 0 {
		t.Errorf("doc = %+v, want one service and no messages", doc)
	}
	// A service name with no dots has no package and is its own short name.
	if doc.Services[0].Name != "GhostService" || doc.Package != "" {
		t.Errorf("service = %+v, package = %q", doc.Services[0], doc.Package)
	}
}

// Constructed AST shapes the parser cannot produce; the walkers must skip them.
func TestCollectInterfacesSkipsNonFuncNamedElement(t *testing.T) {
	file := &ast.File{Decls: []ast.Decl{
		&ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: ast.NewIdent("OddServer"),
				Type: &ast.InterfaceType{Methods: &ast.FieldList{List: []*ast.Field{
					{Names: []*ast.Ident{ast.NewIdent("NotAMethod")}, Type: ast.NewIdent("int")},
				}}},
			},
		}},
	}}
	out := map[string]serverIface{}
	collectInterfaces(file, out)
	if len(out["OddServer"].methods) != 0 {
		t.Errorf("methods = %+v, want none", out["OddServer"].methods)
	}
}

func TestCollectServiceDescsSkipsNonValueSpec(t *testing.T) {
	file := &ast.File{Decls: []ast.Decl{
		&ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
			&ast.TypeSpec{Name: ast.NewIdent("X"), Type: ast.NewIdent("int")},
		}},
	}}
	if got := collectServiceDescs(file); len(got) != 0 {
		t.Errorf("= %+v, want nothing", got)
	}
}

func TestParseServiceDescOddElements(t *testing.T) {
	lit := &ast.CompositeLit{Elts: []ast.Expr{
		ast.NewIdent("positional"), // not a key/value pair
		&ast.KeyValueExpr{ // key that is not an identifier
			Key:   &ast.BasicLit{Kind: token.STRING, Value: `"k"`},
			Value: ast.NewIdent("v"),
		},
	}}
	if d, ok := parseServiceDesc(lit); ok {
		t.Errorf("= %+v, want no desc without a ServiceName", d)
	}
}

func TestHandlerIfaceRejectsOtherShapes(t *testing.T) {
	cases := []ast.Expr{
		ast.NewIdent("notACall"),
		&ast.CallExpr{Fun: ast.NewIdent("f")},                                       // no paren
		&ast.CallExpr{Fun: &ast.ParenExpr{X: ast.NewIdent("T")}},                    // no star
		&ast.CallExpr{Fun: &ast.ParenExpr{X: &ast.StarExpr{X: &ast.SelectorExpr{X: ast.NewIdent("p"), Sel: ast.NewIdent("T")}}}}, // starred non-ident
	}
	for i, e := range cases {
		if got := handlerIface(e); got != "" {
			t.Errorf("case %d: = %q, want empty", i, got)
		}
	}
}

func TestShortNameAndPkgOfWithoutDots(t *testing.T) {
	if got := shortName("Svc"); got != "Svc" {
		t.Errorf("shortName = %q", got)
	}
	if got := pkgOf("Svc"); got != "" {
		t.Errorf("pkgOf = %q", got)
	}
}

func TestWalkFollowsRefs(t *testing.T) {
	all := map[string]*core.Schema{
		"A": {Type: "object", Properties: map[string]*core.Schema{
			"b": {Ref: refPrefix + "B"},
		}},
		"B": {Type: "object"},
	}
	used := map[string]bool{}
	collect("A", all, used)
	if !used["A"] || !used["B"] {
		t.Errorf("used = %v, want A and B", used)
	}
	walk(nil, all, used) // a nil schema is silently ignored
}
