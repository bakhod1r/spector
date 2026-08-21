package gin

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
	"GET":    "get",
	"POST":   "post",
	"PUT":    "put",
	"PATCH":  "patch",
	"DELETE": "delete",
	"HEAD":   "head",
}

type Adapter struct{}

func (a *Adapter) Name() string { return "gin" }

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

	res := astutil.NewResolver(files)
	var diags astutil.Diagnostics
	loc := astutil.Locator{Fset: fset, Dir: dir}
	// scope is how a name, a helper package and a response envelope are
	// resolved. It is shared with every other adapter: none of that is a gin
	// question.
	scope := astutil.NewScope(fset, files)
	groups := astutil.NewGroupResolver(scope, files, res, &diags, loc)

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
			path, ok := res.Resolve(call.Args[0])
			if !ok {
				diags.Add(loc.Position(call.Args[0].Pos()), "route", astutil.DescribeExpr(call.Args[0]))
				return true
			}
			prefix := ""
			if recv, ok := sel.X.(*ast.Ident); ok {
				prefix = groups.Prefix(recv.Name, res.EnclosingFunc(call))
			}
			// A spread argument is a type guarantee: Go only accepts f(x)... in a
			// variadic position when x is a []gin.HandlerFunc, so the call builds a
			// handler chain whatever it is named, and the handler is its last
			// argument — the same idiom as append, without the name allowlist.
			handlerArg := astutil.SpreadArg(call.Args[len(call.Args)-1], call)
			name := astutil.HandlerName(handlerArg)
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

			// The index resolves the handler in the package the registration
			// was written in. Only when it cannot — a name it has never seen —
			// does the flat table decide, which is what a single-package
			// project relies on.
			fd := scope.Handler(handlerArg, handlers)
			// A handler factory names nothing on its own — the argument is a
			// call, not an identifier — so the operation would fall back to a
			// path-derived id. The declaration behind the call is its name.
			if name == "" && fd != nil {
				name = fd.Name.Name
				route.HandlerName = name
			}
			route.Source = loc.Handler(fd, call)
			if fd != nil {
				inspectHandler(fd, &route, scope, scanner.Schemas)
				route.Calls = calls.Analyze(fd, index)
				route.Realtime = realtime.Detect(fd, handlers)
			}
			routes = append(routes, route)
			return true
		})
	}

	return routes, scanner.Schemas, diags.List(), nil
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

func inspectHandler(fd *ast.FuncDecl, route *core.Route, scope *astutil.Scope, schemas map[string]*core.Schema) {
	scope.Inspect(fd, schemas).Apply(route)
	route.Summary, route.Description = astutil.DocComment(fd.Doc, fd.Name.Name)
	applyDirectives(route, fd)
}
