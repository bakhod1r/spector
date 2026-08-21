package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// parseFiles parses named sources as one file set, the way an adapter sees a
// scanned tree.
func parseFiles(t *testing.T, sources ...string) ([]*ast.File, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for i, src := range sources {
		f, err := parser.ParseFile(fset, "f"+string(rune('a'+i))+".go", src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse: %v\nsource:\n%s", err, src)
		}
		files = append(files, f)
	}
	return files, fset
}

// bodyOf returns the body of the named function.
func bodyOf(t *testing.T, files []*ast.File, name string) *ast.BlockStmt {
	t.Helper()
	fd := FuncDecls(files)[name]
	if fd == nil {
		t.Fatalf("no function %q", name)
	}
	return fd.Body
}

const wrapperSrc = `package p

type Req struct{}
type Resp struct{}

func newResp() Resp { return Resp{} }

func bindJSON(c *Ctx, out any) bool {
	if err := c.ShouldBindJSON(out); err != nil {
		c.JSON(400, nil)
		return false
	}
	return true
}

func respond(c *Ctx, code int, body any) { c.JSON(code, body) }

func handler(c *Ctx) {
	var req Req
	if !bindJSON(c, &req) {
		return
	}
	respond(c, 201, newResp())
}
`

// The whole point of stepping into a helper: a handler that never names the
// framework itself documents the same as one that does.
func TestInspectHandlerInFollowsHelpers(t *testing.T) {
	files, _ := parseFiles(t, wrapperSrc)
	pkg := Pkg{Returns: Returns(files), Funcs: FuncDecls(files)}

	h := InspectHandlerIn(bodyOf(t, files, "handler"), pkg)

	if h.Request.Name != "Req" {
		t.Errorf("request = %+v, want Req through bindJSON", h.Request)
	}
	// The response is built by a constructor, so its type comes from the
	// signature rather than the call site.
	if h.Response.Name != "Resp" {
		t.Errorf("response = %+v, want Resp from newResp()", h.Response)
	}
	var statuses []int
	for _, r := range h.Responses {
		statuses = append(statuses, r.Status)
	}
	// 400 is written inside the bind helper; 201 travelled in as an argument
	// and is resolved inside respond, which names it `code`.
	if len(statuses) != 2 || statuses[0] != 400 || statuses[1] != 201 {
		t.Errorf("statuses = %v, want [400 201]", statuses)
	}
}

// A helper that calls itself, or two that call each other, must not walk until
// the stack ends.
func TestInspectHandlerInStopsOnRecursion(t *testing.T) {
	files, _ := parseFiles(t, `package p

type Out struct{}

func ping(c *Ctx) { pong(c) }
func pong(c *Ctx) { ping(c); c.JSON(200, Out{}) }

func handler(c *Ctx) { ping(c) }
`)
	pkg := Pkg{Returns: Returns(files), Funcs: FuncDecls(files)}
	h := InspectHandlerIn(bodyOf(t, files, "handler"), pkg)
	if h.Response.Name != "Out" {
		t.Errorf("response = %+v, want Out", h.Response)
	}
}

// Without an index the inspection is the shallow one, unchanged for callers
// that never asked for more.
func TestInspectHandlerWithoutFuncsStaysShallow(t *testing.T) {
	files, _ := parseFiles(t, wrapperSrc)
	h := InspectHandlerWith(bodyOf(t, files, "handler"), Returns(files))
	if h.Request.Name != "" || len(h.Responses) != 0 {
		t.Errorf("handler = %+v, want nothing without a function index", h)
	}
}

// FuncDecls gives a name to the plain function rather than a method that
// shares it, so a handler `listUsers` is not shadowed by `(*store).listUsers`.
func TestFuncDeclsPrefersFunctionsOverMethods(t *testing.T) {
	files, _ := parseFiles(t, `package p

type store struct{}

func (s *store) listUsers() {}

func listUsers() {}

func (s *store) onlyAMethod() {}
`)
	funcs := FuncDecls(files)
	if fd := funcs["listUsers"]; fd == nil || fd.Recv != nil {
		t.Errorf("listUsers resolved to %v, want the plain function", fd)
	}
	if funcs["onlyAMethod"] == nil {
		t.Error("a method with no function of that name should still be indexed")
	}
}

const groupSrc = `package p

func Register(r *Engine) {
	private := r.Group("/api/v1")
	registerMFA(private.Group("/auth/mfa"))
	admin := private.Group("/admin")
	registerBilling(admin.Group("/billing"), nil)
	registerMFA(r.Group("/other"))
}

func registerMFA(g *Group) {}

func registerBilling(b *Group, _ any) { registerRefunds(b.Group("/refunds")) }

func registerRefunds(r *Group) {}
`

func TestGroupParamsResolvesAndComposes(t *testing.T) {
	files, fset := parseFiles(t, groupSrc)
	res := NewResolver(files)
	funcs := FuncDecls(files)
	var diags Diagnostics
	out := map[*ast.FuncDecl]map[string]string{}

	// Group *variables* are the adapter's business — collectGroups records
	// `private := r.Group("/api/v1")` — and reach here through prefixOf.
	groups := map[string]string{"private": "/api/v1", "admin": "/api/v1/admin"}
	GroupParams(files, res, out, func(name string, _ *ast.FuncDecl) (string, bool) {
		p, ok := groups[name]
		return p, ok
	}, &diags, Locator{Fset: fset, Dir: "."})

	if got := out[funcs["registerMFA"]]["g"]; got != "/api/v1/auth/mfa" {
		t.Errorf("registerMFA g = %q, want /api/v1/auth/mfa", got)
	}
	// Composed through a group variable and a second call hop.
	if got := out[funcs["registerBilling"]]["b"]; got != "/api/v1/admin/billing" {
		t.Errorf("registerBilling b = %q, want /api/v1/admin/billing", got)
	}
	if got := out[funcs["registerRefunds"]]["r"]; got != "/api/v1/admin/billing/refunds" {
		t.Errorf("registerRefunds r = %q, want …/refunds", got)
	}

	// registerMFA is called twice with different prefixes: the first wins and
	// the disagreement is reported rather than silently picked.
	list := diags.List()
	if len(list) != 1 || list[0].Kind != "group-param" {
		t.Fatalf("diagnostics = %+v, want one group-param", list)
	}
	if got := out[funcs["registerMFA"]]["g"]; got != "/api/v1/auth/mfa" {
		t.Errorf("conflict changed the answer to %q", got)
	}
}

// prefixOf is how an adapter contributes the groups it already knows about.
func TestGroupParamsUsesTheCallersTable(t *testing.T) {
	files, fset := parseFiles(t, `package p

func Register(v1 *Group) { sub(v1.Group("/x")) }

func sub(g *Group) {}
`)
	funcs := FuncDecls(files)
	out := map[*ast.FuncDecl]map[string]string{}
	var diags Diagnostics

	GroupParams(files, NewResolver(files), out, func(name string, _ *ast.FuncDecl) (string, bool) {
		if name == "v1" {
			return "/api/v1", true
		}
		return "", false
	}, &diags, Locator{Fset: fset, Dir: "."})

	if got := out[funcs["sub"]]["g"]; got != "/api/v1/x" {
		t.Errorf("sub g = %q, want /api/v1/x", got)
	}
}

// An argument that is not a group at all leaves the parameter alone.
func TestGroupParamsIgnoresOrdinaryArguments(t *testing.T) {
	files, fset := parseFiles(t, `package p

func Register() { sub("literal", 2) }

func sub(a string, b int) {}
`)
	out := map[*ast.FuncDecl]map[string]string{}
	var diags Diagnostics
	GroupParams(files, NewResolver(files), out, nil, &diags, Locator{Fset: fset, Dir: "."})
	if len(out) != 0 {
		t.Errorf("out = %v, want nothing", out)
	}
}

func TestParamNames(t *testing.T) {
	files, _ := parseFiles(t, `package p

func f(a, b string, _ int, c any) {}

func g() {}
`)
	funcs := FuncDecls(files)
	got := paramNames(funcs["f"])
	want := []string{"a", "b", "_", "c"}
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names = %v, want %v", got, want)
		}
	}
	if names := paramNames(funcs["g"]); len(names) != 0 {
		t.Errorf("names = %v, want none", names)
	}
}

// EnclosingFunc is what keeps one function's `r` from meaning another's.
func TestEnclosingFunc(t *testing.T) {
	files, _ := parseFiles(t, `package p

func outer() {
	inner := func() { println("x") }
	inner()
}
`)
	res := NewResolver(files)
	var call *ast.CallExpr
	ast.Inspect(files[0], func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && call == nil {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "println" {
				call = c
			}
		}
		return true
	})
	if call == nil {
		t.Fatal("no call found")
	}
	fd := res.EnclosingFunc(call)
	if fd == nil || fd.Name.Name != "outer" {
		t.Errorf("enclosing = %v, want outer (a literal's enclosing declaration)", fd)
	}
	if fd := res.EnclosingFunc(files[0]); fd != nil {
		t.Errorf("enclosing of a file = %v, want nil", fd)
	}
}

// fiber writes c.Status(201).JSON(body); the code is on the receiver of the
// JSON call, not among its arguments.
func TestChainedStatus(t *testing.T) {
	files, _ := parseFiles(t, `package p

type Out struct{}

func handler(c *Ctx) {
	c.Status(201).JSON(Out{})
	plain().JSON(Out{})
	c.Wrong(201).JSON(Out{})
}

func plain() *Ctx { return nil }
`)
	h := InspectHandlerIn(bodyOf(t, files, "handler"), Pkg{})
	var statuses []int
	for _, r := range h.Responses {
		statuses = append(statuses, r.Status)
	}
	// The chained one is 201; the other two have no readable code and default
	// to 200, which dedupes to a single entry.
	if len(statuses) != 2 || statuses[0] != 201 || statuses[1] != 200 {
		t.Errorf("statuses = %v, want [201 200]", statuses)
	}
}

// The remaining shapes DescribeExpr distinguishes: a field or method value, a
// plain local, and everything else.
func TestDescribeExprRemainingShapes(t *testing.T) {
	cases := map[string]string{
		`s.field.Path`:  "field or method value",
		`localVar`:      "variable value",
		`"a" + fn()`:    "non-literal expression",
		`[]string{"a"}`: "non-literal expression",
	}
	for src, want := range cases {
		if got := DescribeExpr(parseExpr(t, src)); got != want {
			t.Errorf("DescribeExpr(%s) = %q, want %q", src, got, want)
		}
	}
}

// The zero Locator has no file set, so a caller that never built one does not
// have to special-case it.
func TestLocatorPositionWithoutAFileSet(t *testing.T) {
	if pos := (Locator{}).Position(1); pos.Filename != "" || pos.Line != 0 {
		t.Errorf("position = %+v, want the zero value", pos)
	}
}
