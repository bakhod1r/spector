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

	"github.com/user/specter/internal/adapter/astutil"
	"github.com/user/specter/internal/calls"
	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/middleware"
	"github.com/user/specter/internal/realtime"
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

func (a *Adapter) Scan(dir string) ([]core.Route, map[string]*core.Schema, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}

	scanner := core.NewStructScanner()
	index := calls.NewIndex()
	mw := middleware.NewIndex()
	var files []*ast.File
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			files = append(files, file)
			scanner.Collect(file)
			index.Collect(file)
			mw.Collect(file)
		}
	}

	handlers := map[string]*ast.FuncDecl{}
	for _, file := range files {
		for _, decl := range file.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				handlers[fd.Name.Name] = fd
			}
		}
	}

	groups := collectGroups(files)
	loc := astutil.Locator{Fset: fset, Dir: dir}

	var routes []core.Route
	for _, file := range files {
		collectRoutes(file, groups, handlers, &routes, loc, index, mw)
	}

	return routes, scanner.Schemas, nil
}

func collectRoutes(file *ast.File, groups map[string]groupDef, handlers map[string]*ast.FuncDecl, routes *[]core.Route, loc astutil.Locator, index *calls.Index, mw *middleware.Index) {
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
			if path, ok := astutil.StringLit(call.Args[0]); ok {
				handler, inline := splitHandlers(call.Args[1:])
				for _, m := range []string{"get", "post", "put", "patch", "delete"} {
					addRoute(m, path, handler, inline, sel, call, groups, handlers, routes, loc, index, mw)
				}
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
			path, ok := astutil.StringLit(call.Args[1])
			if !ok {
				return true
			}
			handler, inline := splitHandlers(call.Args[2:])
			addRoute(m, path, handler, inline, sel, call, groups, handlers, routes, loc, index, mw)
			return true
		}

		method, ok := methods[sel.Sel.Name]
		if !ok || len(call.Args) < 2 {
			return true
		}
		path, ok := astutil.StringLit(call.Args[0])
		if !ok {
			return true
		}
		handler, inline := splitHandlers(call.Args[1:])
		addRoute(method, path, handler, inline, sel, call, groups, handlers, routes, loc, index, mw)
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
	groups map[string]groupDef, handlers map[string]*ast.FuncDecl, routes *[]core.Route, loc astutil.Locator, index *calls.Index, mw *middleware.Index) {

	if handlerArg == nil {
		return
	}

	prefix := ""
	recvName := ""
	if recv, ok := sel.X.(*ast.Ident); ok {
		recvName = recv.Name
		prefix = resolveGroup(recv.Name, groups)
	}

	name := astutil.HandlerName(handlerArg)
	route := core.Route{
		Method:      method,
		Path:        normalizePath(prefix + path),
		HandlerName: name,
		Middleware:  mw.For(recvName, call.Pos(), inline),
	}
	fd := handlers[name]
	route.Source = loc.Handler(fd, call)
	if fd != nil {
		route.Calls = calls.Analyze(fd, index)
		route.Realtime = realtime.Detect(fd, handlers)
		astutil.InspectHandler(fd.Body).Apply(&route)
		route.Summary, route.Description = astutil.DocComment(fd.Doc, fd.Name.Name)
		d := astutil.ParseDirectives(fd.Doc)
		route.Tags, route.Deprecated, route.OperationID = d.Tags, d.Deprecated, d.OperationID
	}
	*routes = append(*routes, route)
}

type groupDef struct {
	recv   string
	prefix string
}

// collectGroups records `v := app.Group("/prefix", mw...)` so routes registered
// on v resolve to the full path. Groups nest, so each one remembers its
// receiver.
func collectGroups(files []*ast.File) map[string]groupDef {
	groups := map[string]groupDef{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
				return true
			}
			lhs, ok := as.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			call, ok := as.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" || len(call.Args) < 1 {
				return true
			}
			prefix, ok := astutil.StringLit(call.Args[0])
			if !ok {
				return true
			}
			recv := ""
			if id, ok := sel.X.(*ast.Ident); ok {
				recv = id.Name
			}
			groups[lhs.Name] = groupDef{recv: recv, prefix: prefix}
			return true
		})
	}
	return groups
}

func resolveGroup(name string, groups map[string]groupDef) string {
	seen := map[string]bool{}
	prefix := ""
	for {
		g, ok := groups[name]
		if !ok || seen[name] {
			break // a group assigned from itself would otherwise loop
		}
		seen[name] = true
		prefix = g.prefix + prefix
		name = g.recv
	}
	return prefix
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
