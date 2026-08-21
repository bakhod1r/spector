// Package fiber scans gofiber/fiber routing calls. Fiber's shape is close to
// echo's — groups are values assigned to a variable and path params use the
// `:name` syntax — but registration differs in two ways: method names are
// capitalised like chi's (Get, Post), and when several handlers are passed the
// final one is the handler while the preceding ones are inline middleware.
package fiber

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
	"Get":     "get",
	"Post":    "post",
	"Put":     "put",
	"Patch":   "patch",
	"Delete":  "delete",
	"Head":    "head",
	"Options": "options",
}

type Adapter struct{}

func (a *Adapter) Name() string { return "fiber" }

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

		// app.All("/x", h) registers every method; report the common ones so
		// the endpoint is not silently missing from the document.
		if sel.Sel.Name == "All" && len(call.Args) >= 2 {
			if path, ok := res.Resolve(call.Args[0]); ok {
				handler, inline := splitHandlers(call.Args[1:])
				for _, m := range []string{"get", "post", "put", "patch", "delete"} {
					addRoute(m, path, handler, inline, sel, call, groups, handlers, routes, loc, index, mw, scope, schemas)
				}
			} else {
				diags.Add(loc.Position(call.Args[0].Pos()), "route", astutil.DescribeExpr(call.Args[0]))
			}
			return true
		}

		// app.Add("GET", "/x", h) names the method as a value.
		if sel.Sel.Name == "Add" && len(call.Args) >= 3 {
			name, ok := astutil.StringLit(call.Args[0])
			if !ok {
				// fiber.MethodGet and http.MethodGet are selectors.
				if s, ok := call.Args[0].(*ast.SelectorExpr); ok {
					name = strings.TrimPrefix(s.Sel.Name, "Method")
				}
			}
			m, known := methods[capitalize(name)]
			if !known {
				return true
			}
			path, ok := res.Resolve(call.Args[1])
			if !ok {
				diags.Add(loc.Position(call.Args[1].Pos()), "route", astutil.DescribeExpr(call.Args[1]))
				return true
			}
			handler, inline := splitHandlers(call.Args[2:])
			addRoute(m, path, handler, inline, sel, call, groups, handlers, routes, loc, index, mw, scope, schemas)
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
		handler, inline := splitHandlers(call.Args[1:])
		addRoute(method, path, handler, inline, sel, call, groups, handlers, routes, loc, index, mw, scope, schemas)
		return true
	})
}

// splitHandlers separates fiber's variadic registration: the last argument is
// the handler, anything before it is middleware attached to this route alone.
func splitHandlers(args []ast.Expr) (handler ast.Expr, inline []ast.Expr) {
	if len(args) == 0 {
		return nil, nil
	}
	return args[len(args)-1], args[:len(args)-1]
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func addRoute(method, path string, handlerArg ast.Expr, inline []ast.Expr, sel *ast.SelectorExpr, call ast.Node,
	groups *astutil.GroupResolver, handlers map[string]*ast.FuncDecl, routes *[]core.Route, loc astutil.Locator, index *calls.Index, mw *middleware.Index,
	scope *astutil.Scope, schemas map[string]*core.Schema) {

	if handlerArg == nil {
		return
	}

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

// normalizePath converts fiber's `:id`, optional `:id?` and wildcards into
// OpenAPI's `{id}`.
func normalizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		switch {
		case strings.HasPrefix(p, ":"):
			parts[i] = "{" + strings.TrimSuffix(p[1:], "?") + "}"
		case p == "*" || p == "+":
			parts[i] = "{wildcard}"
		}
	}
	return strings.Join(parts, "/")
}
