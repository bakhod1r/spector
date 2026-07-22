package gqlgenx

import (
	"go/ast"
	"os"
	"path/filepath"
	"testing"

	"github.com/user/specter/internal/core"
)

func writeGo(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanMissingDir(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing directory must be an error")
	}
}

func TestScanInvalidGoFile(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "bad.go", "package p\nfunc {")
	if _, err := Scan(dir); err == nil {
		t.Error("an unparsable file must be an error")
	}
}

func TestScanSubscriptionAndOddInterfaceShapes(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "gen.go", `package p

import "context"

type Tick struct {
	At string `+"`json:\"at\"`"+`
}

type SubscriptionResolver interface {
	QueryResolver                                   // embedded: no name to read
	Ticks(context.Context, string) (*Tick, error) // unnamed non-context arg
	Done(ctx context.Context) error                  // only an error to return
	hidden(ctx context.Context) (*Tick, error)       // unexported
}

type QueryResolver interface{}
`)
	doc, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Subscriptions) != 2 {
		t.Fatalf("subscriptions = %+v", doc.Subscriptions)
	}
	ticks := doc.Subscriptions[0]
	if ticks.Name != "Ticks" || ticks.ReturnType != "Tick" {
		t.Errorf("Ticks = %+v", ticks)
	}
	if len(ticks.Args) != 1 || ticks.Args[0].Name != "string" {
		t.Errorf("an unnamed arg takes its type's name: %+v", ticks.Args)
	}
	if done := doc.Subscriptions[1]; done.Name != "Done" || done.ReturnType != "" {
		t.Errorf("Done = %+v", done)
	}
	if doc.Types["Tick"] == nil {
		t.Errorf("Tick model not collected: %+v", doc.Types)
	}
}

// An interface element with a name but a non-function type cannot come out of
// the parser, but resolverFields must still not trip over one.
func TestResolverFieldsSkipsNonFuncNamedElement(t *testing.T) {
	it := &ast.InterfaceType{Methods: &ast.FieldList{List: []*ast.Field{
		{Names: []*ast.Ident{ast.NewIdent("Odd")}, Type: ast.NewIdent("int")},
	}}}
	if got := resolverFields(it, map[string]*core.Schema{}, map[string]bool{}); len(got) != 0 {
		t.Errorf("= %+v, want nothing", got)
	}
}
