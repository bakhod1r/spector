package stdlib

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

func (a *Adapter) Name() string { return "stdlib" }

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

	handlers := map[string]*ast.FuncDecl{}
	for _, file := range files {
		for _, decl := range file.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				handlers[fd.Name.Name] = fd
			}
		}
	}

	mounts := collectMounts(files)
	served := collectServed(files)

	var routes []core.Route
	loc := astutil.Locator{Fset: fset, Dir: dir}
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
			if sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle" {
				return true
			}
			if len(call.Args) != 2 {
				return true
			}
			pattern, ok := astutil.StringLit(call.Args[0])
			if !ok {
				return true
			}
			method, path, ok := splitPattern(pattern)
			if !ok {
				return true
			}
			// net/http has no Use: middleware is applied by wrapping, at
			// three levels that all end up in front of this route.
			var wrappers []ast.Expr
			if recv, ok := sel.X.(*ast.Ident); ok {
				// If this mux was mounted under a prefix, prepend it. This
				// mirrors gin's router groups / chi's Route for versioning:
				//   v1 := http.NewServeMux(); v1.HandleFunc("GET /users", ...)
				//   mux.Handle("/v1/", http.StripPrefix("/v1", v1))
				m := mounts[recv.Name]
				path = m.prefix + path
				// Whatever wraps the server's handler runs in front of every
				// route it serves, including the ones on a mounted sub-mux.
				wrappers = append(wrappers, served[m.parent]...)
				wrappers = append(wrappers, served[recv.Name]...)
				wrappers = append(wrappers, m.wrappers...)
			}
			handlerArg, inline := unwrap(call.Args[1])
			wrappers = append(wrappers, inline...)

			name := astutil.HandlerName(handlerArg)
			route := core.Route{
				Method:      method,
				Path:        path,
				HandlerName: name,
				Middleware:  mw.Chain(wrappers),
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
			routes = append(routes, route)
			return true
		})
	}

	return routes, scanner.Schemas, nil
}

// mount is where a sub-mux is mounted: the prefix its routes hang under, the
// mux it was mounted on, and whatever wraps it there — a guard around a mounted
// sub-mux applies to every route on it.
type mount struct {
	prefix   string
	parent   string
	wrappers []ast.Expr
}

// collectMounts maps a sub-mux variable name to how it is mounted, by scanning
// for mux.Handle("/v1/", <handler referencing the sub-mux>) calls. The handler
// argument may be the sub-mux directly or wrapped in http.StripPrefix(...) and
// middleware; the wrapping is peeled off to find it.
func collectMounts(files []*ast.File) map[string]mount {
	mounts := map[string]mount{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Handle" || len(call.Args) != 2 {
				return true
			}
			prefix, ok := astutil.StringLit(call.Args[0])
			if !ok {
				return true
			}
			inner, wrappers := unwrap(call.Args[1])
			id := astutil.HandlerName(inner)
			if id == "" {
				return true
			}
			parent := ""
			if recv, ok := sel.X.(*ast.Ident); ok {
				parent = recv.Name
			}
			mounts[id] = mount{
				prefix:   strings.TrimSuffix(prefix, "/"),
				parent:   parent,
				wrappers: wrappers,
			}
			return true
		})
	}
	return mounts
}

// collectServed maps a mux variable to what wraps it where it is handed to a
// server. That wrapping runs in front of everything the mux serves, which makes
// it the closest thing net/http has to a global r.Use.
//
//	http.ListenAndServe(":8080", logging(mux))
//	srv := &http.Server{Handler: logging(mux)}
func collectServed(files []*ast.File) map[string][]ast.Expr {
	served := map[string][]ast.Expr{}
	record := func(expr ast.Expr) {
		inner, wrappers := unwrap(expr)
		if id := astutil.HandlerName(inner); id != "" && len(wrappers) > 0 {
			served[id] = append(served[id], wrappers...)
		}
	}

	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch t := n.(type) {
			case *ast.CallExpr:
				sel, ok := t.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "ListenAndServe", "Serve":
					if len(t.Args) == 2 {
						record(t.Args[1])
					}
				case "ListenAndServeTLS":
					if len(t.Args) == 4 {
						record(t.Args[3])
					}
				}
			case *ast.KeyValueExpr:
				// http.Server{Handler: logging(mux)}
				if key, ok := t.Key.(*ast.Ident); ok && key.Name == "Handler" {
					record(t.Value)
				}
			}
			return true
		})
	}
	return served
}

// unwrap peels the middleware off a handler expression, returning the handler
// underneath and the wrappers in the order they run — outermost first.
//
//	logging(auth(handler))  ->  handler, [logging, auth]
//
// Conversions and adapters are not middleware and are skipped, so a handler
// written as http.HandlerFunc(h) or http.StripPrefix("/v1", mux) is not
// reported as running behind something called HandlerFunc.
func unwrap(expr ast.Expr) (handler ast.Expr, wrappers []ast.Expr) {
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return expr, wrappers
		}
		if !transparent(call.Fun) {
			wrappers = append(wrappers, call.Fun)
		}
		// The handler is conventionally the last argument: StripPrefix takes
		// the prefix first, and a middleware constructor its options.
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
	if !ok || pkg.Name != "http" {
		return false
	}
	switch sel.Sel.Name {
	case "HandlerFunc", "StripPrefix", "Handler":
		return true
	}
	return false
}

func splitPattern(pattern string) (method, path string, ok bool) {
	fields := strings.Fields(pattern)
	if len(fields) != 2 {
		return "", "", false
	}
	m, ok := methods[fields[0]]
	if !ok {
		return "", "", false
	}
	return m, normalizeWildcards(fields[1]), true
}

func normalizeWildcards(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			inner := strings.TrimSuffix(p[1:len(p)-1], "...")
			parts[i] = "{" + inner + "}"
		}
	}
	return strings.Join(parts, "/")
}
