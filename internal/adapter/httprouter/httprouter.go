// Package httprouter documents APIs built on julienschmidt/httprouter. The
// router is flat — it has no native groups or Use() the way chi and gin do — so
// the scan is a straight walk of routing calls: the method helpers
// r.GET/POST/... and the generic r.Handle(method, path, handle).
//
// Middleware in httprouter is written as wrapping instead of registration, and
// both forms are read: a handler wrapped at the registration site
// (r.GET("/x", auth(handleX))) and the router itself wrapped where it is served
// (http.ListenAndServe(addr, logging(router))), which applies to every route
// registered on that router.
package httprouter

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
	// spector: directives all live in doc comments, and without this flag
	// fd.Doc is always nil and every one of them is silently lost.
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

	// Router-level wrappers are resolved before the routes are walked: the call
	// that serves the router usually appears below the registrations.
	global := map[string][]ast.Expr{}
	for _, file := range files {
		collectGlobal(file, global)
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
	res := astutil.NewResolver(files)
	var diags astutil.Diagnostics
	w := &walker{handlers: handlers, loc: loc, index: index, mw: mw, global: global, res: res, diags: &diags, scope: astutil.NewScope(fset, files), schemas: scanner.Schemas}
	for _, file := range files {
		w.collect(file)
	}
	return w.routes, scanner.Schemas, diags.List(), nil
}

type walker struct {
	// scope resolves a handler name against the package it was written in,
	// reads it through the project's helper packages, and names the payload
	// inside its response envelope. None of that is framework-specific.
	scope    *astutil.Scope
	schemas  map[string]*core.Schema
	handlers map[string]*ast.FuncDecl
	loc      astutil.Locator
	index    *calls.Index
	mw       *middleware.Index
	// global maps a router variable to the wrappers around it where it is
	// served, outermost first.
	global map[string][]ast.Expr
	routes []core.Route
	res    *astutil.Resolver
	diags  *astutil.Diagnostics
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

		method, path, handler, ok := routingCall(sel, call, w.res, w.loc, w.diags)
		if !ok {
			return true
		}
		w.add(method, path, handler, call, recvName(sel))
		return true
	})
}

// routingCall recognises the two shapes httprouter offers and returns the
// method, path literal and handler expression common to both.
//
//	r.GET("/users", handle)          — method from the selector
//	r.Handle("GET", "/users", handle) — method from the first argument
func routingCall(sel *ast.SelectorExpr, call *ast.CallExpr, res *astutil.Resolver, loc astutil.Locator, diags *astutil.Diagnostics) (method, path string, handler ast.Expr, ok bool) {
	if m, isMethod := methods[sel.Sel.Name]; isMethod && len(call.Args) == 2 {
		if p, ok := res.Resolve(call.Args[0]); ok {
			return m, p, call.Args[1], true
		}
		diags.Add(loc.Position(call.Args[0].Pos()), "route", astutil.DescribeExpr(call.Args[0]))
		return "", "", nil, false
	}
	if sel.Sel.Name == "Handle" && len(call.Args) == 3 {
		verb, ok1 := astutil.StringLit(call.Args[0])
		p, ok2 := res.Resolve(call.Args[1])
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

func (w *walker) add(method, path string, handler ast.Expr, call *ast.CallExpr, recv string) {
	// auth(handleUsers) is a handler behind a middleware, not a handler called
	// auth: the wrappers come off first so the route points at the real one.
	handler, wrappers := unwrap(handler)
	name := astutil.HandlerName(handler)
	route := core.Route{
		Method:      method,
		Path:        normalizePath(path),
		HandlerName: name,
		// The router's own wrappers run outside anything wrapped here.
		Middleware: w.mw.Chain(append(append([]ast.Expr(nil), w.global[recv]...), wrappers...)),
	}
	fd := w.scope.Handler(handler, w.handlers)
	route.Source = w.loc.Handler(fd, call)
	if fd != nil {
		route.Calls = calls.Analyze(fd, w.index)
		route.Realtime = realtime.Detect(fd, w.handlers)
		w.scope.Inspect(fd, w.schemas).Apply(&route)
		route.Summary, route.Description = astutil.DocComment(fd.Doc, fd.Name.Name)
		d := astutil.ParseDirectives(fd.Doc)
		route.Tags, route.Deprecated, route.OperationID = d.Tags, d.Deprecated, d.OperationID
	}
	w.routes = append(w.routes, route)
}

// recvName reports the variable a routing call was made on, which is how a
// route is matched to the wrappers around its router.
func recvName(sel *ast.SelectorExpr) string {
	if id, ok := sel.X.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// collectGlobal records, per router variable, the middleware wrapped around the
// router itself — http.ListenAndServe(addr, logging(auth(router))) or
// srv := &http.Server{Handler: logging(router)}. Those run in front of every
// route the router serves.
//
// Router variables are the ones assigned httprouter.New(); wrapping anything
// else is not this router's business.
func collectGlobal(file *ast.File, out map[string][]ast.Expr) {
	routers := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		lhs, lok := as.Lhs[0].(*ast.Ident)
		if !lok || !isNewRouter(as.Rhs[0]) {
			return true
		}
		routers[lhs.Name] = true
		return true
	})
	if len(routers) == 0 {
		return
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			inner, wrappers := unwrap(arg)
			id, ok := inner.(*ast.Ident)
			if !ok || len(wrappers) == 0 || !routers[id.Name] {
				continue
			}
			out[id.Name] = append(out[id.Name], wrappers...)
		}
		return true
	})
}

// isNewRouter reports whether expr constructs an httprouter router.
func isNewRouter(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "httprouter"
}

// unwrap peels the middleware off a handler expression, returning the handler
// underneath and the wrappers in the order they run — outermost first.
//
//	logging(auth(handler))  ->  handler, [logging, auth]
//
// Conversions and adapters are not middleware and are skipped, so a handler
// written as http.HandlerFunc(h) or httprouter.Handler(h) is not reported as
// running behind something called HandlerFunc.
func unwrap(expr ast.Expr) (handler ast.Expr, wrappers []ast.Expr) {
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return expr, wrappers
		}
		if !transparent(call.Fun) {
			wrappers = append(wrappers, call.Fun)
		}
		// The handler is conventionally the last argument: a middleware
		// constructor takes its options first.
		expr = call.Args[len(call.Args)-1]
	}
}

// transparent reports whether a wrapping call is plumbing rather than
// middleware.
func transparent(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch pkg.Name {
	case "http":
		switch sel.Sel.Name {
		case "HandlerFunc", "StripPrefix", "Handler":
			return true
		}
	case "httprouter":
		// The adapters between httprouter.Handle and net/http handlers.
		switch sel.Sel.Name {
		case "Handler", "HandlerFunc":
			return true
		}
	}
	return false
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
