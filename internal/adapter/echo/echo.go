// Package echo scans labstack/echo routing calls. Echo's shape matches gin's
// closely — groups are values assigned to a variable, and path params use the
// same `:name` syntax — so the two adapters differ mainly in method names and
// handler conventions, which astutil already unifies.
package echo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/bakhod1r/spector/internal/adapter/astutil"
	"github.com/bakhod1r/spector/internal/calls"
	"github.com/bakhod1r/spector/internal/core"
	"github.com/bakhod1r/spector/internal/middleware"
	"github.com/bakhod1r/spector/internal/realtime"
)

var methods = map[string]string{
	"GET":     "get",
	"POST":    "post",
	"PUT":     "put",
	"PATCH":   "patch",
	"DELETE":  "delete",
	"HEAD":    "head",
	"OPTIONS": "options",
}

type Adapter struct{}

func (a *Adapter) Name() string { return "echo" }

func (a *Adapter) Scan(dir string) ([]core.Route, map[string]*core.Schema, []astutil.Diagnostic, error) {
	fset := token.NewFileSet()
	files, err := astutil.ParseDir(fset, dir, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, err
	}

	scanner := core.NewStructScanner()
	index := calls.NewIndex()
	mw := middleware.NewIndex()
	for _, file := range files {
		scanner.Collect(file)
		index.Collect(file)
		mw.Collect(file)
	}

	handlers := map[string]*ast.FuncDecl{}
	for _, file := range files {
		for _, decl := range file.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				handlers[fd.Name.Name] = fd
			}
		}
	}

	loc := astutil.Locator{Fset: fset, Dir: dir}
	// scope resolves handler names against the package they were written in,
	// reads handlers through the project's helper packages, and names the
	// payload inside its response envelope — the parts of a scan that have
	// nothing to do with which framework is in use.
	scope := astutil.NewScope(fset, files)
	res := astutil.NewResolver(files)
	var diags astutil.Diagnostics
	groups := astutil.NewGroupResolver(scope, files, res, &diags, loc)

	var routes []core.Route
	for _, file := range files {
		collectRoutes(file, groups, handlers, &routes, loc, index, mw, res, &diags, scope, scanner.Schemas)
	}

	return routes, scanner.Schemas, diags.List(), nil
}

func collectRoutes(file *ast.File, groups *astutil.GroupResolver, handlers map[string]*ast.FuncDecl, routes *[]core.Route, loc astutil.Locator, index *calls.Index, mw *middleware.Index, res *astutil.Resolver, diags *astutil.Diagnostics,
	scope *astutil.Scope, schemas map[string]*core.Schema) {
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		// e.Any("/x", h) registers every method; report the common ones so the
		// endpoint is not silently missing from the document.
		if sel.Sel.Name == "Any" && len(call.Args) >= 2 {
			if path, ok := res.Resolve(call.Args[0]); ok {
				for _, m := range []string{"get", "post", "put", "patch", "delete"} {
					addRoute(m, path, call.Args[1], call.Args[2:], sel, call, groups, handlers, routes, loc, index, mw, scope, schemas)
				}
			} else {
				diags.Add(loc.Position(call.Args[0].Pos()), "route", astutil.DescribeExpr(call.Args[0]))
			}
			return true
		}

		// e.Match([]string{"GET","POST"}, "/x", h)
		if sel.Sel.Name == "Match" && len(call.Args) >= 3 {
			if path, ok := res.Resolve(call.Args[1]); ok {
				for _, m := range matchMethods(call.Args[0]) {
					addRoute(m, path, call.Args[2], call.Args[3:], sel, call, groups, handlers, routes, loc, index, mw, scope, schemas)
				}
			} else {
				diags.Add(loc.Position(call.Args[1].Pos()), "route", astutil.DescribeExpr(call.Args[1]))
			}
			return true
		}

		method, ok := methods[sel.Sel.Name]
		if !ok || len(call.Args) < 2 {
			return true
		}
		path, ok := res.Resolve(call.Args[0])
		if !ok {
			diags.Add(loc.Position(call.Args[0].Pos()), "route", astutil.DescribeExpr(call.Args[0]))
			return true
		}
		addRoute(method, path, call.Args[1], call.Args[2:], sel, call, groups, handlers, routes, loc, index, mw, scope, schemas)
		return true
	})
}

// addRoute records one route. inline is the middleware attached to the
// registration itself: echo takes it after the handler, where gin takes it
// before, so the slice is worked out by the caller rather than here.
func addRoute(method, path string, handlerArg ast.Expr, inline []ast.Expr, sel *ast.SelectorExpr, call ast.Node,
	groups *astutil.GroupResolver, handlers map[string]*ast.FuncDecl, routes *[]core.Route, loc astutil.Locator, index *calls.Index, mw *middleware.Index,
	scope *astutil.Scope, schemas map[string]*core.Schema) {

	prefix := ""
	recvName := ""
	if recv, ok := sel.X.(*ast.Ident); ok {
		recvName = recv.Name
		prefix = groups.Prefix(recv.Name, scope.EnclosingFunc(call.Pos()))
	}

	if c, ok := call.(*ast.CallExpr); ok {
		handlerArg = astutil.SpreadArg(handlerArg, c)
	}
	name := astutil.HandlerName(handlerArg)
	route := core.Route{
		Method:      method,
		Path:        normalizePath(prefix + path),
		HandlerName: name,
		Middleware:  mw.For(recvName, call.Pos(), inline),
	}
	fd := scope.Handler(handlerArg, handlers)
	route.Source = loc.Handler(fd, call)
	if fd != nil {
		route.Calls = calls.Analyze(fd, index)
		route.Realtime = realtime.Detect(fd, handlers)
		scope.Inspect(fd, schemas).Apply(&route)
		route.Summary, route.Description = astutil.DocComment(fd.Doc, fd.Name.Name)
		d := astutil.ParseDirectives(fd.Doc)
		route.Tags, route.Deprecated, route.OperationID = d.Tags, d.Deprecated, d.OperationID
	}
	*routes = append(*routes, route)
}

// matchMethods pulls the method names out of e.Match([]string{...}, ...).
func matchMethods(expr ast.Expr) []string {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	var out []string
	for _, el := range lit.Elts {
		name, ok := astutil.StringLit(el)
		if !ok {
			// http.MethodGet and friends are selectors, not literals.
			if s, ok := el.(*ast.SelectorExpr); ok {
				name = strings.TrimPrefix(s.Sel.Name, "Method")
			} else {
				continue
			}
		}
		if m, ok := methods[strings.ToUpper(name)]; ok {
			out = append(out, m)
		}
	}
	return out
}

// normalizePath converts echo's `:id` and `*` into OpenAPI's `{id}`.
func normalizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		switch {
		case strings.HasPrefix(p, ":"):
			parts[i] = "{" + p[1:] + "}"
		case p == "*":
			parts[i] = "{wildcard}"
		case strings.HasPrefix(p, "*"):
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}
