package astutil

import (
	"go/ast"
	"go/token"
	"path"
	"path/filepath"
	"strings"
)

// FuncIndex resolves a handler expression to the declaration it names, using
// the package structure of the scanned tree rather than a bare name.
//
// A flat name index is what a single-package example needs and what a real
// project breaks. In a layered codebase `Register` is a route handler in one
// package and a cache subscriber in another, `Login` is both an HTTP handler
// and the service method behind it, and `NewHandler` exists once per bounded
// context. Matching by name alone picks whichever declaration sorted first,
// so an endpoint is documented from an unrelated function's body: the wrong
// summary, the wrong source link, and no request or response type at all.
//
// The index answers the same question with three facts the AST does give us —
// which package a call site is in, what a file's imports are named, and what
// type a receiver has — and only falls back to a bare name when a name is
// globally unique anyway.
type FuncIndex struct {
	// funcs and methods are keyed by package directory. Methods are keyed
	// "Type.Method", which is the pair a selector call actually names.
	funcs   map[string]map[string]*ast.FuncDecl
	methods map[string]map[string]*ast.FuncDecl

	// imports maps a file to the directory each of its import names refers
	// to, so `identityhttp.NewHandler` reaches the right package.
	imports map[*ast.File]map[string]string

	// global counts declarations by bare name across the whole tree, so a
	// name that is unique can still be resolved when nothing else applies.
	global      map[string][]*ast.FuncDecl
	globalMeth  map[string][]*ast.FuncDecl
	dirOfFile   map[*ast.File]string
	fileOfDecl  map[*ast.FuncDecl]*ast.File
	resultOf    map[*ast.FuncDecl]string
	dirOfImport map[string]string
}

// NewFuncIndex builds the index over every parsed file.
func NewFuncIndex(fset *token.FileSet, files []*ast.File) *FuncIndex {
	ix := &FuncIndex{
		funcs:       map[string]map[string]*ast.FuncDecl{},
		methods:     map[string]map[string]*ast.FuncDecl{},
		imports:     map[*ast.File]map[string]string{},
		global:      map[string][]*ast.FuncDecl{},
		globalMeth:  map[string][]*ast.FuncDecl{},
		dirOfFile:   map[*ast.File]string{},
		fileOfDecl:  map[*ast.FuncDecl]*ast.File{},
		resultOf:    map[*ast.FuncDecl]string{},
		dirOfImport: map[string]string{},
	}

	// Pass one: place every declaration in its directory.
	dirs := map[string]bool{}
	for _, f := range files {
		dir := filepath.Dir(fset.Position(f.Pos()).Filename)
		ix.dirOfFile[f] = dir
		dirs[dir] = true
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ix.fileOfDecl[fd] = f
			if t, ok := firstResultOf(fd); ok {
				ix.resultOf[fd] = t.Name
			}
			if fd.Recv == nil {
				put(ix.funcs, dir, fd.Name.Name, fd)
				ix.global[fd.Name.Name] = append(ix.global[fd.Name.Name], fd)
				continue
			}
			recv := recvTypeName(fd.Recv)
			if recv == "" {
				continue
			}
			key := recv + "." + fd.Name.Name
			put(ix.methods, dir, key, fd)
			ix.globalMeth[key] = append(ix.globalMeth[key], fd)
			ix.global[fd.Name.Name] = append(ix.global[fd.Name.Name], fd)
		}
	}

	// Pass two: map import paths to the directories in the tree. The module
	// path is unknown without reading go.mod, so a path is matched by its
	// longest trailing segment run against a directory — which is exactly
	// what a Go import path is relative to the module root.
	for _, f := range files {
		names := map[string]string{}
		for _, spec := range f.Imports {
			p, ok := StringLit(spec.Path)
			if !ok {
				continue
			}
			dir, ok := ix.dirForImport(p, dirs)
			if !ok {
				continue
			}
			name := path.Base(p)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			if name == "_" || name == "." {
				continue
			}
			names[name] = dir
		}
		if len(names) > 0 {
			ix.imports[f] = names
		}
	}
	return ix
}

func put(m map[string]map[string]*ast.FuncDecl, dir, key string, fd *ast.FuncDecl) {
	if m[dir] == nil {
		m[dir] = map[string]*ast.FuncDecl{}
	}
	if _, taken := m[dir][key]; !taken {
		m[dir][key] = fd
	}
}

// dirForImport finds the scanned directory an import path refers to, by
// matching the path's trailing segments against the directory's. Results are
// cached because a large tree repeats the same imports in every file.
func (ix *FuncIndex) dirForImport(importPath string, dirs map[string]bool) (string, bool) {
	if dir, seen := ix.dirOfImport[importPath]; seen {
		return dir, dir != ""
	}
	segs := strings.Split(importPath, "/")
	best, bestLen := "", 0
	for dir := range dirs {
		n := matchingSuffix(dir, segs)
		// One segment is not evidence: every tree has a dozen `http`
		// directories. Two or more is a real path.
		if n >= 2 && n > bestLen {
			best, bestLen = dir, n
		}
	}
	ix.dirOfImport[importPath] = best
	return best, best != ""
}

// matchingSuffix counts how many trailing segments of an import path the
// directory ends with.
func matchingSuffix(dir string, segs []string) int {
	parts := strings.Split(filepath.ToSlash(dir), "/")
	n := 0
	for n < len(segs) && n < len(parts) {
		if parts[len(parts)-1-n] != segs[len(segs)-1-n] {
			break
		}
		n++
	}
	return n
}

// firstResultOf is firstResult guarded for a declaration that returns nothing.
func firstResultOf(fd *ast.FuncDecl) (TypeInfo, bool) {
	if fd.Type == nil || fd.Type.Results == nil {
		return TypeInfo{}, false
	}
	return firstResult(fd.Type.Results)
}

func recvTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	return TypeName(recv.List[0].Type).Name
}

// Lookup resolves the function a handler expression names, from the file and
// enclosing function the expression appears in.
//
// enclosing may be nil, and file may be nil; the index then falls back to a
// globally unique name, which is what a single-package project has anyway.
func (ix *FuncIndex) Lookup(file *ast.File, enclosing *ast.FuncDecl, expr ast.Expr) *ast.FuncDecl {
	return ix.lookupAt(file, enclosing, expr, 0)
}

func (ix *FuncIndex) lookupAt(file *ast.File, enclosing *ast.FuncDecl, expr ast.Expr, depth int) *ast.FuncDecl {
	if ix == nil || depth >= recvTypeDepth {
		return nil
	}
	switch t := HandlerExpr(expr).(type) {
	case *ast.Ident:
		return ix.lookupPlain(file, t.Name)
	case *ast.SelectorExpr:
		return ix.lookupSelector(file, enclosing, t, depth)
	case *ast.CallExpr:
		// A handler factory: the registration passes the call, and the
		// declaration that matters is the one being called.
		return ix.lookupAt(file, enclosing, t.Fun, depth+1)
	}
	return nil
}

func (ix *FuncIndex) lookupPlain(file *ast.File, name string) *ast.FuncDecl {
	// Same package first: a bare call names a declaration in its own package
	// or a dot-import, and the former is what real code has.
	if file != nil {
		if fd, ok := ix.funcs[ix.dirOfFile[file]][name]; ok {
			return fd
		}
	}
	return unique(ix.global[name])
}

func (ix *FuncIndex) lookupSelector(file *ast.File, enclosing *ast.FuncDecl, sel *ast.SelectorExpr, depth int) *ast.FuncDecl {
	name := sel.Sel.Name

	// pkg.Func — the receiver names an import of this file.
	if id, ok := sel.X.(*ast.Ident); ok && file != nil {
		if dir, isPkg := ix.imports[file][id.Name]; isPkg {
			if fd, ok := ix.funcs[dir][name]; ok {
				return fd
			}
		}
	}

	// value.Method — resolve what the value is, then take that type's method
	// in the package the type came from.
	if dir, typeName, ok := ix.recvTypeAt(file, enclosing, sel.X, depth+1); ok {
		key := typeName + "." + name
		if fd, found := ix.methods[dir][key]; found {
			return fd
		}
		// The value's package could not be pinned down, but the Type.Method
		// pair still names one declaration in nearly every tree.
		if fd := unique(ix.globalMeth[key]); fd != nil {
			return fd
		}
	}

	// Same package, then a globally unique bare name.
	if file != nil {
		dir := ix.dirOfFile[file]
		if fd, ok := ix.funcs[dir][name]; ok {
			return fd
		}
		for key, fd := range ix.methods[dir] {
			if strings.HasSuffix(key, "."+name) {
				return fd
			}
		}
	}
	return unique(ix.global[name])
}

// recvTypeDepth bounds recvTypeAt. Values reach a route registration through
// one or two hops — a receiver, or a local built by a constructor — and the
// bound is what stops source that assigns two names from each other,
//
//	a := b.Group("/a")
//	b := a.Group("/b")
//
// from walking until the stack ends. Such code does not compile, but a scanner
// reads what is on disk, including a file mid-edit.
const recvTypeDepth = 8

// recvTypeAt reports the package directory and type name of the value an
// expression denotes, for the shapes a route registration actually uses:
// a receiver, a parameter, a local built by a constructor, and a constructor
// called inline.
func (ix *FuncIndex) recvTypeAt(file *ast.File, enclosing *ast.FuncDecl, expr ast.Expr, depth int) (string, string, bool) {
	if depth >= recvTypeDepth {
		return "", "", false
	}
	switch x := expr.(type) {
	case *ast.CallExpr:
		// sessionhttp.NewHandler(c.Session).Mount(...)
		fd := ix.lookupAt(file, enclosing, x.Fun, depth+1)
		if fd == nil {
			return "", "", false
		}
		t, ok := ix.resultOf[fd]
		if !ok || t == "" {
			return "", "", false
		}
		return ix.dirOfFile[ix.fileOfDecl[fd]], t, true

	case *ast.Ident:
		if enclosing == nil {
			return "", "", false
		}
		dir := ix.dirOfFile[file]
		// A receiver or a parameter states its type directly.
		if t, ok := declaredType(enclosing, x.Name); ok {
			return dir, t, true
		}
		// A local assigned from a constructor: identity := pkg.NewHandler(...)
		if rhs, ok := assignedValue(enclosing.Body, x.Name); ok {
			return ix.recvTypeAt(file, enclosing, rhs, depth+1)
		}
	}
	return "", "", false
}

// declaredType reports the type name a receiver or parameter of fd carries.
func declaredType(fd *ast.FuncDecl, name string) (string, bool) {
	lists := []*ast.FieldList{fd.Recv, fd.Type.Params}
	for _, list := range lists {
		if list == nil {
			continue
		}
		for _, f := range list.List {
			for _, id := range f.Names {
				if id.Name == name {
					if t := TypeName(f.Type).Name; t != "" {
						return t, true
					}
				}
			}
		}
	}
	return "", false
}

// assignedValue returns the right-hand side of the first assignment to name in
// body. Only a single-value assignment is considered: anything else does not
// name one constructor result.
func assignedValue(body *ast.BlockStmt, name string) (ast.Expr, bool) {
	var out ast.Expr
	ast.Inspect(body, func(n ast.Node) bool {
		if out != nil {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name == name {
			out = as.Rhs[0]
			return false
		}
		return true
	})
	return out, out != nil
}

func unique(decls []*ast.FuncDecl) *ast.FuncDecl {
	if len(decls) == 1 {
		return decls[0]
	}
	return nil
}

// HandlerBody is the block a handler declaration actually serves requests
// from.
//
// For an ordinary handler that is the function's own body. For a handler
// factory — a function that closes over a value and returns the handler —
//
//	func (h *Handler) Verify(ch valueobject.Channel) gin.HandlerFunc {
//		return func(c *gin.Context) { ... }
//	}
//
// the function's body contains no request handling at all: it contains a
// return statement. Reading it documents the endpoint with nothing, which is
// what every parameterised route in a project got. The returned literal is
// the handler, so that is what is inspected.
//
// The outer body is not discarded: a factory that binds or responds before
// returning is rare but legal, and both blocks are reported so the caller can
// inspect each.
func HandlerBody(fd *ast.FuncDecl) []*ast.BlockStmt {
	if fd == nil || fd.Body == nil {
		return nil
	}
	if !returnsFunc(fd) {
		return []*ast.BlockStmt{fd.Body}
	}
	var out []*ast.BlockStmt
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range ret.Results {
			if lit, ok := r.(*ast.FuncLit); ok && lit.Body != nil {
				out = append(out, lit.Body)
			}
		}
		return true
	})
	if len(out) == 0 {
		return []*ast.BlockStmt{fd.Body}
	}
	return out
}

// returnsFunc reports whether fd's first result is a function — either written
// out (func(c *gin.Context)) or named (gin.HandlerFunc, http.HandlerFunc).
func returnsFunc(fd *ast.FuncDecl) bool {
	if fd.Type == nil || fd.Type.Results == nil || len(fd.Type.Results.List) == 0 {
		return false
	}
	switch t := fd.Type.Results.List[0].Type.(type) {
	case *ast.FuncType:
		return true
	case *ast.SelectorExpr:
		// gin.HandlerFunc, http.HandlerFunc, echo.HandlerFunc, fiber.Handler.
		return handlerFuncNames[t.Sel.Name]
	case *ast.Ident:
		// A bare name is only trusted when it cannot mean anything else.
		// `Handler` is what half the projects in Go call their HTTP handler
		// *struct*, and treating a constructor as a factory would document an
		// endpoint from whatever closure the constructor happened to build.
		return t.Name == "HandlerFunc"
	}
	return false
}

// handlerFuncNames are the named function types a framework calls a handler,
// as spelled through its package. A factory returning any other named type is
// not a handler factory, and stepping into its literal would document somebody
// else's callback.
var handlerFuncNames = map[string]bool{
	"HandlerFunc":   true,
	"Handler":       true, // fiber.Handler
	"HandlersChain": true,
}

// DirOf is the package directory a declaration was written in.
func (ix *FuncIndex) DirOf(fd *ast.FuncDecl) string {
	if ix == nil || fd == nil {
		return ""
	}
	return ix.dirOfFile[ix.fileOfDecl[fd]]
}

// PkgAt is the inspection context for a call site: the result types and
// declarations of the package the handler itself lives in, so a handler is
// read against its own package rather than against a flattened tree.
func (ix *FuncIndex) PkgAt(fd *ast.FuncDecl, fallback Pkg) Pkg {
	if ix == nil || fd == nil {
		return fallback
	}
	file, ok := ix.fileOfDecl[fd]
	if !ok {
		return fallback
	}
	dir := ix.dirOfFile[file]
	funcs := map[string]*ast.FuncDecl{}
	// The handler's own package wins every name; the rest of the tree fills
	// in helpers it calls across package boundaries (httpx.OK and friends),
	// which is where the request and response types of most real handlers
	// live.
	for k, v := range fallback.Funcs {
		funcs[k] = v
	}
	for name, decl := range ix.funcs[dir] {
		funcs[name] = decl
	}
	for key, decl := range ix.methods[dir] {
		funcs[key[strings.Index(key, ".")+1:]] = decl
	}
	// A helper reached through an import of the handler's file resolves to
	// that package's declaration, not to whichever tree-wide name won.
	for _, imported := range ix.imports[file] {
		for name, decl := range ix.funcs[imported] {
			if _, taken := ix.funcs[dir][name]; !taken {
				funcs[name] = decl
			}
		}
	}
	return Pkg{Returns: fallback.Returns, Funcs: funcs}
}
