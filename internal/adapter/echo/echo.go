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

	"github.com/user/specter/internal/adapter/astutil"
	"github.com/user/specter/internal/calls"
	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/middleware"
	"github.com/user/specter/internal/realtime"
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

		// e.Any("/x", h) registers every method; report the common ones so the
		// endpoint is not silently missing from the document.
		if sel.Sel.Name == "Any" && len(call.Args) >= 2 {
			if path, ok := astutil.StringLit(call.Args[0]); ok {
				for _, m := range []string{"get", "post", "put", "patch", "delete"} {
					addRoute(m, path, call.Args[1], call.Args[2:], sel, call, groups, handlers, routes, loc, index, mw)
				}
			}
			return true
		}

		// e.Match([]string{"GET","POST"}, "/x", h)
		if sel.Sel.Name == "Match" && len(call.Args) >= 3 {
			if path, ok := astutil.StringLit(call.Args[1]); ok {
				for _, m := range matchMethods(call.Args[0]) {
					addRoute(m, path, call.Args[2], call.Args[3:], sel, call, groups, handlers, routes, loc, index, mw)
				}
			}
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
		addRoute(method, path, call.Args[1], call.Args[2:], sel, call, groups, handlers, routes, loc, index, mw)
		return true
	})
}

// addRoute records one route. inline is the middleware attached to the
// registration itself: echo takes it after the handler, where gin takes it
// before, so the slice is worked out by the caller rather than here.
func addRoute(method, path string, handlerArg ast.Expr, inline []ast.Expr, sel *ast.SelectorExpr, call ast.Node,
	groups map[string]groupDef, handlers map[string]*ast.FuncDecl, routes *[]core.Route, loc astutil.Locator, index *calls.Index, mw *middleware.Index) {

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

type groupDef struct {
	recv   string
	prefix string
}

// collectGroups records `v := e.Group("/prefix")` so routes registered on v
// resolve to the full path. Groups nest, so each one remembers its receiver.
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
