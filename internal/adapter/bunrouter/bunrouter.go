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

func (a *Adapter) Name() string { return "bunrouter" }

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
	w := &walker{handlers: handlers, loc: loc, index: index, mw: mw, res: res, diags: &diags, scope: astutil.NewScope(fset, files), schemas: scanner.Schemas, entered: map[*ast.FuncDecl]bool{}}
	for _, file := range files {
		// Middleware passed to the constructor — bunrouter.New(bunrouter.Use(a))
		// — is in front of everything that router serves, so it opens the scope
		// the file's routes are walked with.
		w.collect(file, "", constructorMiddleware(file))
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
	routes   []core.Route
	res      *astutil.Resolver
	diags    *astutil.Diagnostics
	// entered guards a registration function that is handed the router and
	// hands it on again, directly or through another.
	entered map[*ast.FuncDecl]bool
}

// collect walks node for bunrouter routing calls under the accumulated prefix.
// WithGroup("/api", func(g *bunrouter.Group){...}) nests its body under the
// extended prefix, giving bunrouter the same versioning support chi has.
//
// scope is the middleware in effect here, outermost first. bunrouter's groups
// are closures that shadow the router variable, so — as with chi — middleware
// cannot be resolved by the name it was registered on and is tracked as the
// walk descends instead. Order still decides: a Use is added to the scope where
// it appears, so routes registered above it are unaffected.
func (w *walker) collect(node ast.Node, prefix string, scope []ast.Expr) {
	w.collectIn(node, prefix, scope, nil)
}

// collectIn is collect with the names the router is known by at this point,
// which is what lets the walk follow a group handed to another function.
func (w *walker) collectIn(node ast.Node, prefix string, scope []ast.Expr, routers map[string]bool) {
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		// r.Use(mw) and r.WithMiddleware(mw) as a statement of their own.
		if isUse(sel.Sel.Name) {
			scope = append(scope[:len(scope):len(scope)], call.Args...)
			return true
		}

		// Groups: WithGroup("/api", func(g){...}) with a prefix, and Group(func(g){...})
		// without one (bunrouter's middleware-only group). Either may be reached
		// through r.Use(mw).WithGroup(...), which scopes that middleware to the
		// group alone.
		if body, p, name, ok := groupBody(sel, call, w.res, w.loc, w.diags); ok {
			w.collectIn(body, prefix+p, append(scope[:len(scope):len(scope)], receiverMiddleware(sel)...),
				astutil.WithRouter(routers, name))
			return false // inner routes already handled with the prefix
		}

		// The group is handed to a registration function elsewhere; its routes
		// belong under this prefix.
		if callee, param, ok := w.scope.RouterHandoff(call, routers); ok && !w.entered[callee] {
			w.entered[callee] = true
			w.collectIn(callee.Body, prefix, scope, astutil.WithRouter(nil, param))
			delete(w.entered, callee)
			return false
		}

		method, ok := methods[sel.Sel.Name]
		if !ok || len(call.Args) != 2 {
			return true
		}
		path, ok := w.res.Resolve(call.Args[0])
		if !ok {
			w.diags.Add(w.loc.Position(call.Args[0].Pos()), "route", astutil.DescribeExpr(call.Args[0]))
			return true
		}
		// r.Use(mw).GET(...) attaches middleware to this route alone.
		w.add(method, prefix+path, call.Args[1], call, append(scope[:len(scope):len(scope)], receiverMiddleware(sel)...))
		// The subtree holds only the handler and, for a chained receiver, the
		// Use call already accounted for — descending would add that middleware
		// to the surrounding scope, where it does not belong.
		return false
	})
}

func (w *walker) add(method, path string, handler ast.Expr, call *ast.CallExpr, scope []ast.Expr) {
	name := astutil.HandlerName(handler)
	route := core.Route{
		Method:      method,
		Path:        normalizePath(path),
		HandlerName: name,
		Middleware:  w.mw.Chain(scope),
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

// groupBody reports the closure a bunrouter group runs and the prefix it adds.
// WithGroup carries a path; Group carries none and exists to scope middleware.
func groupBody(sel *ast.SelectorExpr, call *ast.CallExpr, res *astutil.Resolver, loc astutil.Locator, diags *astutil.Diagnostics) (body *ast.BlockStmt, prefix, router string, ok bool) {
	switch {
	case sel.Sel.Name == "WithGroup" && len(call.Args) == 2:
		p, ok := res.Resolve(call.Args[0])
		if !ok {
			diags.Add(loc.Position(call.Args[0].Pos()), "group-prefix", astutil.DescribeExpr(call.Args[0]))
			return nil, "", "", false
		}
		fn, ok := call.Args[1].(*ast.FuncLit)
		if !ok {
			return nil, "", "", false
		}
		return fn.Body, p, astutil.FuncLitParam(fn), true
	case sel.Sel.Name == "Group" && len(call.Args) == 1:
		fn, ok := call.Args[0].(*ast.FuncLit)
		if !ok {
			return nil, "", "", false
		}
		return fn.Body, "", astutil.FuncLitParam(fn), true
	}
	return nil, "", "", false
}

// isUse reports whether a selector registers middleware. bunrouter spells it
// both ways: Use adds to what is already there, WithMiddleware starts a chain.
func isUse(name string) bool { return name == "Use" || name == "WithMiddleware" }

// receiverMiddleware pulls the middleware out of r.Use(a).Use(b).GET(...),
// where the receiver of the call is itself a Use call. The chain is returned
// outermost first, which is the order it was written in.
func receiverMiddleware(sel *ast.SelectorExpr) []ast.Expr {
	var out []ast.Expr
	expr := sel.X
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			break
		}
		inner, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !isUse(inner.Sel.Name) {
			break
		}
		out = append(call.Args[:len(call.Args):len(call.Args)], out...)
		expr = inner.X
	}
	return out
}

// constructorMiddleware reports the middleware handed to bunrouter.New, which
// is how a bunrouter router gets its global chain:
//
//	router := bunrouter.New(bunrouter.Use(reqlog.NewMiddleware(...)))
func constructorMiddleware(file *ast.File) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "New" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "bunrouter" {
			return true
		}
		for _, arg := range call.Args {
			opt, ok := arg.(*ast.CallExpr)
			if !ok {
				continue
			}
			fn, ok := opt.Fun.(*ast.SelectorExpr)
			if !ok || !isUse(fn.Sel.Name) {
				continue
			}
			out = append(out, opt.Args...)
		}
		return true
	})
	return out
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
