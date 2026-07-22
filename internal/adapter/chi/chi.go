package chi

import (
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/user/specter/internal/adapter/astutil"
	"github.com/user/specter/internal/calls"
	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/realtime"
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
	var routes []core.Route
	for _, file := range files {
		collectRoutes(file, "", handlers, &routes, loc, index)
	}

	return routes, scanner.Schemas, nil
}

// collectRoutes walks node for chi routing calls under the accumulated path
// prefix. r.Route("/api", func(r chi.Router){...}) and r.Mount("/api", sub)
// nest their bodies under the extended prefix, giving chi the same
// router-group / versioning support gin has.
func collectRoutes(node ast.Node, prefix string, handlers map[string]*ast.FuncDecl, routes *[]core.Route, loc astutil.Locator, index *calls.Index) {
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// Group: r.Route("/api", func(r chi.Router){ ... }).
		if sel.Sel.Name == "Route" && len(call.Args) == 2 {
			if p, ok := astutil.StringLit(call.Args[0]); ok {
				if fn, ok := call.Args[1].(*ast.FuncLit); ok {
					collectRoutes(fn.Body, prefix+p, handlers, routes, loc, index)
					return false // inner routes already handled with the prefix
				}
			}
		}
		method, ok := methods[sel.Sel.Name]
		if !ok || len(call.Args) != 2 {
			return true
		}
		path, ok := astutil.StringLit(call.Args[0])
		if !ok {
			return true
		}
		name := astutil.HandlerName(call.Args[1])
		route := core.Route{
			Method:      method,
			Path:        prefix + path,
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
		*routes = append(*routes, route)
		return true
	})
}
