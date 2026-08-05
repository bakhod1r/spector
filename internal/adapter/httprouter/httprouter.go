// Package httprouter documents APIs built on julienschmidt/httprouter. The
// router is flat — it has no native groups or per-route middleware the way chi
// and gin do — so the scan is a straight walk of routing calls: the method
// helpers r.GET/POST/... and the generic r.Handle(method, path, handle).
package httprouter

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

// The method helpers httprouter exposes. Handle(method, path, h) is handled
// separately because its method is an argument rather than the selector name.
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

func (a *Adapter) Name() string { return "httprouter" }

func (a *Adapter) Scan(dir string) ([]core.Route, map[string]*core.Schema, []astutil.Diagnostic, error) {
	fset := token.NewFileSet()
	// ParseComments is required, not optional: summaries, descriptions and the
	// specter: directives all live in doc comments, and without this flag
	// fd.Doc is always nil and every one of them is silently lost.
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, err
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
	consts := astutil.StringConsts(files)
	var diags astutil.Diagnostics
	w := &walker{handlers: handlers, loc: loc, index: index, consts: consts, diags: &diags}
	for _, file := range files {
		w.collect(file)
	}
	return w.routes, scanner.Schemas, diags.List(), nil
}

type walker struct {
	handlers map[string]*ast.FuncDecl
	loc      astutil.Locator
	index    *calls.Index
	routes   []core.Route
	consts   map[string]string
	diags    *astutil.Diagnostics
}

func (w *walker) collect(node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		method, path, handler, ok := routingCall(sel, call, w.consts, w.loc, w.diags)
		if !ok {
			return true
		}
		w.add(method, path, handler, call)
		return true
	})
}

// routingCall recognises the two shapes httprouter offers and returns the
// method, path literal and handler expression common to both.
//
//	r.GET("/users", handle)          — method from the selector
//	r.Handle("GET", "/users", handle) — method from the first argument
func routingCall(sel *ast.SelectorExpr, call *ast.CallExpr, consts map[string]string, loc astutil.Locator, diags *astutil.Diagnostics) (method, path string, handler ast.Expr, ok bool) {
	if m, isMethod := methods[sel.Sel.Name]; isMethod && len(call.Args) == 2 {
		if p, ok := astutil.ResolveString(call.Args[0], consts); ok {
			return m, p, call.Args[1], true
		}
		diags.Add(loc.Position(call.Args[0].Pos()), "route", astutil.DescribeExpr(call.Args[0]))
		return "", "", nil, false
	}
	if sel.Sel.Name == "Handle" && len(call.Args) == 3 {
		verb, ok1 := astutil.StringLit(call.Args[0])
		p, ok2 := astutil.ResolveString(call.Args[1], consts)
		m, known := methods[strings.ToUpper(verb)]
		if ok1 && ok2 && known {
			return m, p, call.Args[2], true
		}
		if ok1 && known && !ok2 {
			diags.Add(loc.Position(call.Args[1].Pos()), "route", astutil.DescribeExpr(call.Args[1]))
		}
	}
	return "", "", nil, false
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

// normalizePath rewrites httprouter's :name and *catchall segments into the
// OpenAPI {name} form.
func normalizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") || strings.HasPrefix(p, "*") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}
