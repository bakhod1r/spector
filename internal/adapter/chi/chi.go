package chi

import (
	"go/ast"
	"go/parser"
	"go/token"

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

func (a *Adapter) Name() string { return "chi" }

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
		w.collect(file, "", nil)
	}

	return w.routes, scanner.Schemas, diags.List(), nil
}

// walker carries the state that does not change as the walk descends, so the
// recursion only passes what does: the path prefix and the middleware in scope.
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

// collect walks node for chi routing calls under the accumulated path prefix.
// r.Route("/api", func(r chi.Router){...}) and r.Mount("/api", sub) nest their
// bodies under the extended prefix, giving chi the same router-group /
// versioning support gin has.
//
// scope is the middleware in effect here, outermost first. chi's groups are
// closures that shadow the router variable, so middleware cannot be resolved by
// the name it was registered on the way gin's is — what is in scope is tracked
// as the walk descends instead. Order still decides: an r.Use is added to the
// scope where it appears, so routes registered above it are unaffected.
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

		if sel.Sel.Name == "Use" {
			scope = append(scope[:len(scope):len(scope)], call.Args...)
			return true
		}

		// Groups: r.Route("/api", func(r chi.Router){...}) with a prefix, and
		// r.Group(func(r chi.Router){...}) without one — the latter exists
		// precisely to scope middleware.
		if body, p, name, ok := groupBody(sel, call, w.res, w.loc, w.diags); ok {
			w.collectIn(body, prefix+p, scope, astutil.WithRouter(routers, name))
			return false // inner routes already handled with the prefix
		}

		// The group is handed to a registration function elsewhere; its
		// routes belong under this prefix.
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
		handlerArg := call.Args[1]
		name := astutil.HandlerName(handlerArg)
		route := core.Route{
			Method:      method,
			Path:        prefix + path,
			HandlerName: name,
			// r.With(mw).Get(...) attaches middleware to this route alone.
			Middleware: w.mw.Chain(append(scope[:len(scope):len(scope)], withArgs(sel)...)),
		}
		fd := w.scope.Handler(handlerArg, w.handlers)
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
		return true
	})
}

// groupBody reports the closure a chi group runs, and the prefix it adds.
// Route carries a path; Group carries none and exists to scope middleware.
func groupBody(sel *ast.SelectorExpr, call *ast.CallExpr, res *astutil.Resolver, loc astutil.Locator, diags *astutil.Diagnostics) (body *ast.BlockStmt, prefix, router string, ok bool) {
	switch {
	case sel.Sel.Name == "Route" && len(call.Args) == 2:
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

// withArgs pulls the middleware out of r.With(a, b).Get(...), where the
// receiver of the routing call is itself the With call.
func withArgs(sel *ast.SelectorExpr) []ast.Expr {
	call, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return nil
	}
	inner, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || inner.Sel.Name != "With" {
		return nil
	}
	return call.Args
}
