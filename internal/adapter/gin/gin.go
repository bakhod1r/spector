package gin

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
	"GET":    "get",
	"POST":   "post",
	"PUT":    "put",
	"PATCH":  "patch",
	"DELETE": "delete",
	"HEAD":   "head",
}

type Adapter struct{}

func (a *Adapter) Name() string { return "gin" }

func (a *Adapter) Scan(dir string) ([]core.Route, map[string]*core.Schema, error) {
	fset := token.NewFileSet()
	// ParseComments is required, not optional: summaries, descriptions and the
	// specter: directives all live in doc comments, and without this flag
	// fd.Doc is always nil and every one of them is silently lost.
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

	// Handlers are indexed by name, and a method must never displace a plain
	// function that shares it. Real code has both — a handler listUsers next to
	// a store method (*store).listUsers — and since files are walked in map
	// order, letting the method win would make the scan non-deterministic:
	// the same source would document a response type on one run and none on
	// the next.
	//
	// A route registers a plain function (v1.GET("/users", listUsers)), so the
	// function is the right owner of the bare name. Methods are still indexed,
	// but only where no function has claimed it.
	handlers := map[string]*ast.FuncDecl{}
	byMethodName := map[string]*ast.FuncDecl{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fd.Recv != nil {
				if _, taken := byMethodName[fd.Name.Name]; !taken {
					byMethodName[fd.Name.Name] = fd
				}
				continue
			}
			handlers[fd.Name.Name] = fd
		}
	}
	for name, fd := range byMethodName {
		if _, taken := handlers[name]; !taken {
			handlers[name] = fd
		}
	}

	groups := collectGroups(files)
	loc := astutil.Locator{Fset: fset, Dir: dir}
	// Handlers that return a value from a store or service — the normal shape
	// in real code — need the package's function result types to be
	// documentable at all.
	returns := astutil.Returns(files)

	var routes []core.Route
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
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
			prefix := ""
			if recv, ok := sel.X.(*ast.Ident); ok {
				prefix = resolveGroup(recv.Name, groups)
			}
			name := astutil.HandlerName(call.Args[len(call.Args)-1])
			route := core.Route{
				Method:      method,
				Path:        normalizePath(prefix + path),
				HandlerName: name,
			}
			// Everything except the last argument is middleware attached to
			// this route alone; the last is the handler.
			var inline []ast.Expr
			if len(call.Args) > 2 {
				inline = call.Args[1 : len(call.Args)-1]
			}
			route.Middleware = mw.For(recvName(sel), call.Pos(), inline)

			fd := handlers[name]
			route.Source = loc.Handler(fd, call)
			if fd != nil {
				inspectHandler(fd, &route, returns)
				route.Calls = calls.Analyze(fd, index)
				route.Realtime = realtime.Detect(fd, handlers)
			}
			routes = append(routes, route)
			return true
		})
	}

	return routes, scanner.Schemas, nil
}

type groupDef struct {
	recv   string
	prefix string
}

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
			break
		}
		seen[name] = true
		prefix = g.prefix + prefix
		name = g.recv
	}
	return prefix
}

// recvName is the router variable a route was registered on, which is what
// decides which middleware applies to it.
func recvName(sel *ast.SelectorExpr) string {
	if id, ok := sel.X.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// applyDirectives copies the optional specter: doc-comment directives onto the
// route. They are the only thing in Specter a project has to write by hand, and
// they stay optional: nothing here runs if the comment has none.
func applyDirectives(route *core.Route, fd *ast.FuncDecl) {
	d := astutil.ParseDirectives(fd.Doc)
	route.Tags = d.Tags
	route.Deprecated = d.Deprecated
	route.OperationID = d.OperationID
}

func normalizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") || strings.HasPrefix(p, "*") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func inspectHandler(fd *ast.FuncDecl, route *core.Route, returns map[string]astutil.TypeInfo) {
	astutil.InspectHandlerWith(fd.Body, returns).Apply(route)
	route.Summary, route.Description = astutil.DocComment(fd.Doc, fd.Name.Name)
	applyDirectives(route, fd)
}
