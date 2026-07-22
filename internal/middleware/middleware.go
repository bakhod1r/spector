// Package middleware infers what runs in front of a handler.
//
// This closes the one gap that AST analysis of handlers cannot: whether a route
// is protected. Authentication almost never appears in the handler — it happens
// in a middleware registered on the router, so a generator that reads only
// handler bodies documents every endpoint as public, including the ones that
// answer 401 to everybody.
//
// Two things make this inferable without a type checker:
//
//   - middleware is attached positionally (r.Use(x) applies to what is
//     registered after it, on that router and on groups derived from it), and
//     positions are exactly what the AST has;
//   - middleware is named by people for other people. A function called
//     AuthRequired, JWTMiddleware or RequireAPIKey says what it does.
//
// The second is a convention, so what it produces is reported as a guess. The
// first is structure, and is reported as fact.
package middleware

import (
	"go/ast"
	"go/token"
	"sort"
	"strings"

	"github.com/user/specter/internal/core"
)

// Kinds of middleware worth reporting. Anything else is recorded by name
// without a kind: knowing that RequestID runs is still useful context, but it
// says nothing about the API's contract.
const (
	KindAuth      = "auth"
	KindCORS      = "cors"
	KindRateLimit = "ratelimit"
	KindLogging   = "logging"
	KindRecovery  = "recovery"
	KindCompress  = "compression"
)

// Chain is the middleware in front of one route, outermost first.
type Chain []core.Middleware

// Index holds the middleware registrations found in a package, ready to be
// queried per route. Like the call graph's index it is per-scan state, not a
// package-level map: two scans must not see each other's routers.
type Index struct {
	// uses are Use() calls keyed by the router variable they were called on.
	uses map[string][]registration
	// groups maps a group variable to the router it came from, plus any
	// middleware passed inline at creation.
	groups map[string]group
	// decls maps a middleware's name to its declaration, so its body can be
	// read. Methods are keyed by method name alone: m.Handler() is written that
	// way at the call site, and there is no type checker here to do better.
	decls map[string]*ast.FuncDecl
}

type registration struct {
	names []string
	pos   token.Pos
}

type group struct {
	parent string
	inline []string
	pos    token.Pos
}

func NewIndex() *Index {
	return &Index{uses: map[string][]registration{}, groups: map[string]group{}, decls: map[string]*ast.FuncDecl{}}
}

// Collect records the middleware registrations in a file.
func (ix *Index) Collect(file *ast.File) {
	if ix == nil {
		return
	}
	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			ix.decls[fd.Name.Name] = fd
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		// r.Use(a, b)
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Use" {
				if recv, ok := sel.X.(*ast.Ident); ok {
					ix.uses[recv.Name] = append(ix.uses[recv.Name], registration{
						names: exprNames(call.Args),
						pos:   call.Pos(),
					})
				}
			}
		}
		// v1 := r.Group("/api/v1", authRequired)
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 {
			lhs, lok := as.Lhs[0].(*ast.Ident)
			call, cok := as.Rhs[0].(*ast.CallExpr)
			if !lok || !cok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Group" && sel.Sel.Name != "Route") {
				return true
			}
			parent := ""
			if id, ok := sel.X.(*ast.Ident); ok {
				parent = id.Name
			}
			// The first argument is the path prefix; the rest are middleware.
			var inline []string
			if len(call.Args) > 1 {
				inline = exprNames(call.Args[1:])
			}
			ix.groups[lhs.Name] = group{parent: parent, inline: inline, pos: call.Pos()}
		}
		return true
	})
}

// For returns the middleware in front of a route registered on router recv at
// position pos, with any middleware passed on the registration itself.
//
// Ordering matters and is honoured: r.Use(x) affects only what is registered
// after it. A generator that ignored this would attribute a middleware to
// routes declared above it, which is the kind of error nobody checks because
// the document looks plausible.
func (ix *Index) For(recv string, pos token.Pos, routeArgs []ast.Expr) Chain {
	if ix == nil {
		return nil
	}
	var names []string
	names = append(names, ix.inherited(recv, pos, map[string]bool{})...)
	names = append(names, exprNames(routeArgs)...)

	seen := map[string]bool{}
	var chain Chain
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		chain = append(chain, ix.describe(name))
	}
	return chain
}

// describe combines what a middleware is called with what it does. The name is
// a convention; the body is evidence, so anything the body reveals overrides
// the guess made from the name.
func (ix *Index) describe(name string) core.Middleware {
	m := Classify(name)

	// m.Handler() and jwt.Auth() are written with a qualifier at the call site
	// but declared under the bare name.
	lookup := name
	if i := strings.LastIndex(lookup, "."); i >= 0 {
		lookup = lookup[i+1:]
	}
	fd := ix.decls[lookup]
	if fd == nil {
		return m
	}

	m.Headers, m.Statuses = analyze(fd)

	// A middleware whose name says nothing may still be unmistakably a guard.
	if m.Kind == "" && len(m.Headers) > 0 {
		if scheme, _, ok := authFromHeaders(m.Headers); ok {
			m.Kind, m.Scheme = KindAuth, scheme
		}
	}
	return m
}

// inherited walks from a group up to the root router, collecting the Use calls
// that were in effect at pos.
func (ix *Index) inherited(recv string, pos token.Pos, visited map[string]bool) []string {
	if recv == "" || visited[recv] {
		return nil
	}
	visited[recv] = true

	var out []string
	if g, isGroup := ix.groups[recv]; isGroup {
		// The parent's middleware was in effect when the group was created, so
		// the group's own position is what counts against the parent's Use
		// calls — not the route's, which is further down.
		out = append(out, ix.inherited(g.parent, g.pos, visited)...)
		out = append(out, g.inline...)
	}

	regs := append([]registration(nil), ix.uses[recv]...)
	sort.Slice(regs, func(i, j int) bool { return regs[i].pos < regs[j].pos })
	for _, reg := range regs {
		if reg.pos < pos {
			out = append(out, reg.names...)
		}
	}
	return out
}

// exprNames renders middleware arguments as names. A call like jwt.Auth() is
// reported by its function name, since that is what identifies it.
func exprNames(args []ast.Expr) []string {
	var out []string
	for _, arg := range args {
		if name := exprName(arg); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func exprName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			return pkg.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.CallExpr:
		// AuthRequired("admin") or jwt.Auth(cfg): the function being called is
		// the middleware.
		return exprName(t.Fun)
	case *ast.FuncLit:
		// An inline closure has no name to report, but its presence is still
		// worth recording: something runs here.
		return "func literal"
	}
	return ""
}
