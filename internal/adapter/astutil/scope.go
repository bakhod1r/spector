package astutil

import (
	"go/ast"
	"go/token"
	"sort"

	"github.com/bakhod1r/spector/internal/core"
)

// Scope is everything a scan needs to turn a route registration into a
// documented operation: which declaration a handler expression names, which
// package that declaration should be read against, and what an endpoint really
// returns once its project's response envelope is accounted for.
//
// It exists so that every adapter answers those questions the same way. The
// framework decides how a route is spelled; none of it decides how Go resolves
// a name, how a helper package works, or what an envelope is. Those were
// solved once in the gin adapter and were wrong everywhere else — a chi or
// echo project with the same ordinary helper package got no request types, no
// response types and no schemas at all.
type Scope struct {
	Fset  *token.FileSet
	Index *FuncIndex
	Pkg   Pkg

	// structs is every struct declaration, grouped by the package it is in:
	// the wire name of each field, for rewriting a response envelope, and the
	// element type behind each indexable field, so a handler that answers out
	// of its receiver's own store names a payload.
	structs *StructIndex

	// decls is every function in a file, ordered by position, so the function
	// enclosing a route registration can be found without a parent map.
	decls map[string][]*ast.FuncDecl

	// byName is the parsed file each position belongs to, so an adapter never
	// has to thread the file through its own route-collecting helpers.
	byName map[string]*ast.File
}

// NewScope builds the resolution context for one scan.
func NewScope(fset *token.FileSet, files []*ast.File) *Scope {
	s := &Scope{
		Fset:    fset,
		Index:   NewFuncIndex(fset, files),
		Pkg:     Pkg{Returns: Returns(files), Funcs: FuncDecls(files)},
		structs: NewStructIndex(fset, files),
		decls:   map[string][]*ast.FuncDecl{},
		byName:  map[string]*ast.File{},
	}
	for _, f := range files {
		name := fset.Position(f.Pos()).Filename
		s.byName[name] = f
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				s.decls[name] = append(s.decls[name], fd)
			}
		}
	}
	for _, list := range s.decls {
		sort.Slice(list, func(i, j int) bool { return list[i].Pos() < list[j].Pos() })
	}
	return s
}

// EnclosingFunc reports the function a position lies in, or nil for a position
// outside one. Adapters that already track the enclosing function can pass it
// straight to Handler instead.
func (s *Scope) EnclosingFunc(pos token.Pos) *ast.FuncDecl {
	if s == nil || s.Fset == nil || !pos.IsValid() {
		return nil
	}
	list := s.decls[s.Fset.Position(pos).Filename]
	i := sort.Search(len(list), func(i int) bool { return list[i].Pos() > pos })
	if i == 0 {
		return nil
	}
	if fd := list[i-1]; pos < fd.End() {
		return fd
	}
	return nil
}

// FileAt is the parsed file a position lies in.
func (s *Scope) FileAt(pos token.Pos) *ast.File {
	if s == nil || s.Fset == nil || !pos.IsValid() {
		return nil
	}
	return s.byName[s.Fset.Position(pos).Filename]
}

// Handler resolves the declaration a route's handler argument names, seeing
// through the wrappings a registration puts around it and resolving the name
// in the package it was written in.
//
// The file and the enclosing function are taken from the expression's own
// position, so an adapter does not have to thread either through its route
// walk — several of them do not track the file at all.
//
// fallback is the adapter's own name table, consulted only when the index
// cannot decide — which is the case for a single-package project, where a bare
// name is unambiguous anyway.
func (s *Scope) Handler(expr ast.Expr, fallback map[string]*ast.FuncDecl) *ast.FuncDecl {
	if s == nil || expr == nil {
		return fallback[HandlerName(expr)]
	}
	pos := expr.Pos()
	if fd := s.Index.Lookup(s.FileAt(pos), s.EnclosingFunc(pos), expr); fd != nil {
		return fd
	}
	return fallback[HandlerName(expr)]
}

// Inspect reads a handler declaration into the facts a route carries: the
// request it binds, the responses it writes, the parameters it reads.
//
// schemas may be nil. When it is not, responses written through a project's
// response envelope are given a schema naming the payload, and schemas gains
// that entry.
func (s *Scope) Inspect(fd *ast.FuncDecl, schemas map[string]*core.Schema) Handler {
	if fd == nil {
		return Handler{}
	}
	pkg := Pkg{}
	var structs *StructIndex
	var dir string
	if s != nil {
		pkg = s.Index.PkgAt(fd, s.Pkg)
		// The receiver's own package decides what its fields are; two contexts
		// both calling their handler `Handler` is the normal case, not an
		// exception.
		dir = s.Index.DirOf(fd)
		pkg.RecvFields = s.structs.Elems(dir, recvTypeName(fd.Recv))
		// Calls out of the handler are resolved by the same rules the handler
		// itself was, from the position of the call — which is inside whatever
		// helper the walk has reached, not inside the handler.
		pkg.Resolve = func(call *ast.CallExpr) *ast.FuncDecl {
			pos := call.Pos()
			return s.Index.Lookup(s.FileAt(pos), s.EnclosingFunc(pos), call.Fun)
		}
		structs = s.structs
	}
	h := InspectBodiesIn(HandlerBody(fd), pkg)
	if schemas == nil {
		return h
	}
	return envelopes{schemas: schemas, structs: structs, dir: dir}.apply(h)
}

// RouterHandoff reports a call that hands the router being walked to another
// function, and the name that function knows it by.
//
// Closure-based routers — chi, bunrouter — give a group no variable outside
// the closure it is scoped to, so a project splits its registration up by
// passing that closure's router on:
//
//	r.Route("/api/v1", func(api chi.Router) {
//		users.NewHandler().Mount(api)
//		orders.NewHandler().Mount(api)
//	})
//
// The routes are then registered in another package entirely, and a walk that
// stops at the closure documents them at their bare paths — or, when two
// contexts register the same bare path, loses one of them outright.
//
// routers is the set of names the router is known by at this point in the
// walk. seen guards a registration function that hands the router on to
// itself.
func (s *Scope) RouterHandoff(call *ast.CallExpr, routers map[string]bool) (fd *ast.FuncDecl, param string, ok bool) {
	if s == nil || len(routers) == 0 {
		return nil, "", false
	}
	idx := -1
	for i, arg := range call.Args {
		if id, isIdent := arg.(*ast.Ident); isIdent && routers[id.Name] {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, "", false
	}
	pos := call.Pos()
	callee := s.Index.Lookup(s.FileAt(pos), s.EnclosingFunc(pos), call.Fun)
	if callee == nil || callee.Body == nil {
		return nil, "", false
	}
	names := paramNames(callee)
	if idx >= len(names) || names[idx] == "" || names[idx] == "_" {
		return nil, "", false
	}
	return callee, names[idx], true
}

// FuncLitParam is the name a group closure gives its router:
// r.Route("/api", func(api chi.Router) { ... }) — "api".
func FuncLitParam(fn *ast.FuncLit) string {
	if fn == nil || fn.Type == nil || fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return ""
	}
	names := fn.Type.Params.List[0].Names
	if len(names) == 0 {
		return ""
	}
	return names[0].Name
}

// WithRouter adds a name to the set the router is known by, without touching
// the caller's set: a nested group shadows the name, it does not rename the
// outer one.
func WithRouter(routers map[string]bool, name string) map[string]bool {
	if name == "" || name == "_" {
		return routers
	}
	out := make(map[string]bool, len(routers)+1)
	for k := range routers {
		out[k] = true
	}
	out[name] = true
	return out
}
