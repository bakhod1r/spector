package stdlib

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

	mountPrefix := collectMounts(files)

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
			// If this mux was mounted under a prefix, prepend it. This
			// mirrors gin's router groups / chi's Route for versioning:
			//   v1 := http.NewServeMux(); v1.HandleFunc("GET /users", ...)
			//   mux.Handle("/v1/", http.StripPrefix("/v1", v1))
			if recv, ok := sel.X.(*ast.Ident); ok {
				path = mountPrefix[recv.Name] + path
			}
			name := astutil.HandlerName(call.Args[1])
			route := core.Route{
				Method:      method,
				Path:        path,
				HandlerName: name,
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

// collectMounts maps a sub-mux variable name to the path prefix it is mounted
// under, by scanning for mux.Handle("/v1/", <handler referencing the sub-mux>)
// calls. The handler argument may be the sub-mux directly or wrapped in
// http.StripPrefix(...); the last argument of a wrapping call is unwrapped.
func collectMounts(files []*ast.File) map[string]string {
	mounts := map[string]string{}
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
			if id := mountIdent(call.Args[1]); id != "" {
				mounts[id] = strings.TrimSuffix(prefix, "/")
			}
			return true
		})
	}
	return mounts
}

// mountIdent extracts the sub-mux variable name from a mount handler argument,
// unwrapping wrappers like http.StripPrefix(prefix, mux) to their last arg.
func mountIdent(arg ast.Expr) string {
	switch a := arg.(type) {
	case *ast.Ident:
		return a.Name
	case *ast.CallExpr:
		if len(a.Args) > 0 {
			return mountIdent(a.Args[len(a.Args)-1])
		}
	}
	return ""
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
