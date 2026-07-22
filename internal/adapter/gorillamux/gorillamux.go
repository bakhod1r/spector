// Package gorillamux scans gorilla/mux routing calls. Registration is a chain
// rather than a method name — r.HandleFunc("/x", h).Methods("GET") — and paths
// already use OpenAPI's `{id}` syntax, optionally with a regex (`{id:[0-9]+}`)
// that the document does not want. Subrouters play the role groups do
// elsewhere: s := r.PathPrefix("/api").Subrouter().
package gorillamux

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

func (a *Adapter) Name() string { return "gorillamux" }

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

	subrouters := collectSubrouters(files)
	loc := astutil.Locator{Fset: fset, Dir: dir}

	// A HandleFunc that is the receiver of a Methods chain is reported by the
	// chain, not on its own — otherwise the same route appears twice, once per
	// declared method and once methodless.
	chained := map[*ast.CallExpr]bool{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if reg, _ := methodsChain(n); reg != nil {
				chained[reg] = true
			}
			return true
		})
	}

	var routes []core.Route
	add := func(reg *ast.CallExpr, declared []string) {
		sel, ok := reg.Fun.(*ast.SelectorExpr)
		if !ok || len(reg.Args) < 2 {
			return
		}
		if sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle" {
			return
		}
		path, ok := astutil.StringLit(reg.Args[0])
		if !ok {
			return
		}

		prefix := ""
		recvName := ""
		if recv, ok := sel.X.(*ast.Ident); ok {
			recvName = recv.Name
			prefix = resolvePrefix(recv.Name, subrouters)
		}

		if len(declared) == 0 {
			// No .Methods means the route answers everything; get is the
			// honest minimum rather than inventing five entries.
			declared = []string{"get"}
		}
		name := astutil.HandlerName(reg.Args[1])
		for _, m := range declared {
			route := core.Route{
				Method:      m,
				Path:        normalizePath(prefix + path),
				HandlerName: name,
				Middleware:  mw.For(recvName, reg.Pos(), nil),
			}
			fd := handlers[name]
			route.Source = loc.Handler(fd, reg)
			if fd != nil {
				route.Calls = calls.Analyze(fd, index)
				route.Realtime = realtime.Detect(fd, handlers)
				astutil.InspectHandler(fd.Body).Apply(&route)
				route.Summary, route.Description = astutil.DocComment(fd.Doc, fd.Name.Name)
				d := astutil.ParseDirectives(fd.Doc)
				route.Tags, route.Deprecated, route.OperationID = d.Tags, d.Deprecated, d.OperationID
			}
			routes = append(routes, route)
		}
	}

	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if reg, declared := methodsChain(n); reg != nil {
				add(reg, declared)
				return true
			}
			if call, ok := n.(*ast.CallExpr); ok && !chained[call] {
				add(call, nil)
			}
			return true
		})
	}

	return routes, scanner.Schemas, nil
}

// methodsChain unpicks r.HandleFunc(...).Methods("GET", "POST"): it returns
// the inner registration call and the lower-cased methods, or nil when node is
// not such a chain.
func methodsChain(n ast.Node) (*ast.CallExpr, []string) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return nil, nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Methods" {
		return nil, nil
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return nil, nil
	}
	var out []string
	for _, arg := range call.Args {
		name, ok := astutil.StringLit(arg)
		if !ok {
			// http.MethodGet and friends are selectors, not literals.
			if s, ok := arg.(*ast.SelectorExpr); ok {
				name = strings.TrimPrefix(s.Sel.Name, "Method")
			} else {
				continue
			}
		}
		if m, ok := methods[strings.ToUpper(name)]; ok {
			out = append(out, m)
		}
	}
	return inner, out
}

type subDef struct {
	recv   string
	prefix string
}

// collectSubrouters records `s := r.PathPrefix("/api").Subrouter()` so routes
// registered on s resolve to the full path. Subrouters nest, so each one
// remembers its receiver.
func collectSubrouters(files []*ast.File) map[string]subDef {
	subs := map[string]subDef{}
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
			if !ok || sel.Sel.Name != "Subrouter" {
				return true
			}
			inner, ok := sel.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			innerSel, ok := inner.Fun.(*ast.SelectorExpr)
			if !ok || innerSel.Sel.Name != "PathPrefix" || len(inner.Args) < 1 {
				return true
			}
			prefix, ok := astutil.StringLit(inner.Args[0])
			if !ok {
				return true
			}
			recv := ""
			if id, ok := innerSel.X.(*ast.Ident); ok {
				recv = id.Name
			}
			subs[lhs.Name] = subDef{recv: recv, prefix: prefix}
			return true
		})
	}
	return subs
}

func resolvePrefix(name string, subs map[string]subDef) string {
	seen := map[string]bool{}
	prefix := ""
	for {
		s, ok := subs[name]
		if !ok || seen[name] {
			break // a subrouter assigned from itself would otherwise loop
		}
		seen[name] = true
		prefix = s.prefix + prefix
		name = s.recv
	}
	return prefix
}

// normalizePath strips the regex out of `{id:[0-9]+}` — OpenAPI wants `{id}`.
func normalizePath(path string) string {
	var b strings.Builder
	depth := 0
	skipping := false
	for _, r := range path {
		switch {
		case r == '{':
			depth++
			b.WriteRune(r)
		case r == '}':
			depth--
			skipping = false
			b.WriteRune(r)
		case r == ':' && depth > 0:
			skipping = true
		case skipping:
			// inside {name:regex}, after the colon — dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
