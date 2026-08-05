// Package bunrouter documents APIs built on uptrace/bunrouter. Routing looks
// like chi's: method helpers r.GET/POST/... register handlers, and groups nest
// a path prefix — bunrouter spells the closure form WithGroup("/api", fn) and
// the compact form NewGroup("/api"). Only the closure form nests structurally,
// which is what this adapter follows.
package bunrouter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/user/specter/internal/adapter/astutil"
	"github.com/user/specter/internal/calls"
	"github.com/user/specter/internal/core"
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

func (a *Adapter) Name() string { return "bunrouter" }

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
	var files []*ast.File
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			files = append(files, file)
			scanner.Collect(file)
			index.Collect(file)
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

	loc := astutil.Locator{Fset: fset, Dir: dir}
	w := &walker{handlers: handlers, loc: loc, index: index}
	for _, file := range files {
		w.collect(file, "")
	}
	return w.routes, scanner.Schemas, nil
}

type walker struct {
	handlers map[string]*ast.FuncDecl
	loc      astutil.Locator
	index    *calls.Index
	routes   []core.Route
}

// collect walks node for bunrouter routing calls under the accumulated prefix.
// WithGroup("/api", func(g *bunrouter.Group){...}) nests its body under the
// extended prefix, giving bunrouter the same versioning support chi has.
func (w *walker) collect(node ast.Node, prefix string) {
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		// Groups: WithGroup("/api", func(g){...}) with a prefix, and Group(func(g){...})
		// without one (bunrouter's middleware-only group).
		if body, p, ok := groupBody(sel, call); ok {
			w.collect(body, prefix+p)
			return false // inner routes already handled with the prefix
		}

		method, ok := methods[sel.Sel.Name]
		if !ok || len(call.Args) != 2 {
			return true
		}
		path, ok := astutil.StringLit(call.Args[0])
		if !ok {
			return true
		}
		w.add(method, prefix+path, call.Args[1], call)
		return true
	})
}

func (w *walker) add(method, path string, handler ast.Expr, call *ast.CallExpr) {
	name := astutil.HandlerName(handler)
	route := core.Route{
		Method:      method,
		Path:        normalizePath(path),
		HandlerName: name,
	}
	fd := w.handlers[name]
	route.Source = w.loc.Handler(fd, call)
	if fd != nil {
		route.Calls = calls.Analyze(fd, w.index)
		route.Realtime = realtime.Detect(fd, w.handlers)
		astutil.InspectHandler(fd.Body).Apply(&route)
		route.Summary, route.Description = astutil.DocComment(fd.Doc, fd.Name.Name)
		d := astutil.ParseDirectives(fd.Doc)
		route.Tags, route.Deprecated, route.OperationID = d.Tags, d.Deprecated, d.OperationID
	}
	w.routes = append(w.routes, route)
}

// groupBody reports the closure a bunrouter group runs and the prefix it adds.
// WithGroup carries a path; Group carries none and exists to scope middleware.
func groupBody(sel *ast.SelectorExpr, call *ast.CallExpr) (body *ast.BlockStmt, prefix string, ok bool) {
	switch {
	case sel.Sel.Name == "WithGroup" && len(call.Args) == 2:
		p, ok := astutil.StringLit(call.Args[0])
		if !ok {
			return nil, "", false
		}
		fn, ok := call.Args[1].(*ast.FuncLit)
		if !ok {
			return nil, "", false
		}
		return fn.Body, p, true
	case sel.Sel.Name == "Group" && len(call.Args) == 1:
		fn, ok := call.Args[0].(*ast.FuncLit)
		if !ok {
			return nil, "", false
		}
		return fn.Body, "", true
	}
	return nil, "", false
}

// normalizePath rewrites bunrouter's :name and *name segments into the OpenAPI
// {name} form.
func normalizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") || strings.HasPrefix(p, "*") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}
