package astutil

import (
	"go/ast"
)

// binding is a local-name candidate value: either a resolved string, or
// masked (declared locally but not statically resolvable — must never fall
// back to a package-level const of the same name).
type binding struct {
	val    string
	masked bool
}

// Resolver resolves string expressions, taking function-local variable
// scope into account. Unlike the package-level ResolveString/StringConsts,
// it correctly masks locally-declared names that cannot be statically
// resolved instead of silently falling back to a same-named package const.
type Resolver struct {
	pkg    map[string]string
	parent map[ast.Node]ast.Node
	envs   map[ast.Node]map[string]binding
}

// NewResolver builds a Resolver over files. It computes package-level
// string consts/vars once and builds a parent index for scope walking.
func NewResolver(files []*ast.File) *Resolver {
	r := &Resolver{
		pkg:    StringConsts(files),
		parent: map[ast.Node]ast.Node{},
		envs:   map[ast.Node]map[string]binding{},
	}
	for _, f := range files {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				return true
			}
			if len(stack) > 0 {
				r.parent[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
	}
	return r
}

// Resolve statically evaluates expr, taking enclosing function-local scope
// into account.
func (r *Resolver) Resolve(expr ast.Expr) (string, bool) {
	chain := r.funcChain(expr)
	if len(chain) == 0 {
		return resolve(expr, r.pkg)
	}
	env := r.envFor(chain)
	return r.eval(expr, env)
}

// funcChain walks parent links from n upward, collecting every enclosing
// *ast.FuncDecl/*ast.FuncLit, innermost first.
func (r *Resolver) funcChain(n ast.Node) []ast.Node {
	var chain []ast.Node
	cur := ast.Node(n)
	for {
		p, ok := r.parent[cur]
		if !ok {
			break
		}
		switch p.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			chain = append(chain, p)
		}
		cur = p
	}
	return chain
}

// envFor builds (or fetches memoized) the combined binding env for the
// innermost func in chain (chain[0]), processing outermost→innermost so
// inner names shadow outer ones.
func (r *Resolver) envFor(chain []ast.Node) map[string]binding {
	innermost := chain[0]
	if env, ok := r.envs[innermost]; ok {
		return env
	}
	env := map[string]binding{}
	for k, v := range r.pkg {
		env[k] = binding{val: v}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		body := funcBody(chain[i])
		if body == nil {
			continue
		}
		r.applyLocalBindings(body, env)
	}
	r.envs[innermost] = env
	return env
}

func funcBody(n ast.Node) *ast.BlockStmt {
	switch f := n.(type) {
	case *ast.FuncDecl:
		return f.Body
	case *ast.FuncLit:
		return f.Body
	}
	return nil
}

// applyLocalBindings extracts local variable candidates from body (not
// descending into nested FuncLit bodies) and resolves them to a fixed
// point against env, mutating env in place.
func (r *Resolver) applyLocalBindings(body *ast.BlockStmt, env map[string]binding) {
	type cand struct {
		name string
		expr ast.Expr
	}
	names := map[string]bool{}
	var cands []cand
	maskedNames := map[string]bool{}

	addCand := func(name string, expr ast.Expr) {
		names[name] = true
		cands = append(cands, cand{name: name, expr: expr})
	}
	mask := func(name string) {
		names[name] = true
		maskedNames[name] = true
	}

	// nestedDeclared collects names that are DECLARED (":=" / var) anywhere
	// below the function's own top-level statement list. Such a name is
	// lexically scoped to that nested block and must never be treated as a
	// function-level binding: a bare reference to the name outside/after
	// that block may lexically resolve to an outer/package binding of the
	// same name instead, and we cannot tell without full positional scope
	// tracking. Conservative fix: mask it, never resolve it.
	nestedDeclared := map[string]bool{}
	var walkNested func(n ast.Node)
	walkNested = func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.FuncLit:
				return false // nested func is its own scope entirely
			case *ast.RangeStmt:
				if id, ok := s.Key.(*ast.Ident); ok && id.Name != "_" {
					nestedDeclared[id.Name] = true
				}
				if id, ok := s.Value.(*ast.Ident); ok && id.Name != "_" {
					nestedDeclared[id.Name] = true
				}
			case *ast.AssignStmt:
				if s.Tok.String() == ":=" {
					for _, lhs := range s.Lhs {
						if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
							nestedDeclared[id.Name] = true
						}
					}
				}
			case *ast.DeclStmt:
				gd, ok := s.Decl.(*ast.GenDecl)
				if !ok {
					return true
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						if name.Name != "_" {
							nestedDeclared[name.Name] = true
						}
					}
				}
			}
			return true
		})
	}
	for _, stmt := range body.List {
		// Any statement that is itself a nested block (if/for/switch/select/
		// bare block/etc.) may contain declarations lexically scoped to it.
		// Walk everything below the top-level statement itself (but not the
		// statement's own top-level init/assign, handled below) to collect
		// those nested names.
		switch stmt.(type) {
		case *ast.AssignStmt, *ast.DeclStmt:
			// handled as a top-level candidate below; nothing nested here
			// (a plain top-level := / var has no sub-block to recurse into
			// for declarations).
		default:
			walkNested(stmt)
		}
	}

	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.FuncLit:
			return false // nested func gets its own env
		case *ast.RangeStmt:
			if id, ok := s.Key.(*ast.Ident); ok && id.Name != "_" {
				mask(id.Name)
			}
			if id, ok := s.Value.(*ast.Ident); ok && id.Name != "_" {
				mask(id.Name)
			}
		case *ast.AssignStmt:
			if (s.Tok.String() == ":=" || s.Tok.String() == "=") && len(s.Lhs) == len(s.Rhs) {
				for i, lhs := range s.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || id.Name == "_" {
						continue
					}
					addCand(id.Name, s.Rhs[i])
				}
			} else {
				for _, lhs := range s.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
						mask(id.Name)
					}
				}
			}
		case *ast.DeclStmt:
			gd, ok := s.Decl.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if len(vs.Names) == len(vs.Values) {
					for i, name := range vs.Names {
						if name.Name == "_" {
							continue
						}
						addCand(name.Name, vs.Values[i])
					}
				} else {
					for _, name := range vs.Names {
						if name.Name != "_" {
							mask(name.Name)
						}
					}
				}
			}
		}
		return true
	})

	// Any name declared in a nested block is forced to masked, regardless
	// of whether a same-named top-level candidate also exists (the top-level
	// candidate and the nested one are different lexical bindings; we can't
	// safely tell which one a given use-site sees without positional scope
	// tracking, so mask conservatively).
	for name := range nestedDeclared {
		mask(name)
	}

	// Group candidates per name to detect conflicting resolutions.
	byName := map[string][]ast.Expr{}
	for _, c := range cands {
		byName[c.name] = append(byName[c.name], c.expr)
	}

	// Resolve to a fixed point: a local may depend on an earlier local.
	resolved := map[string]binding{}
	for name := range names {
		if maskedNames[name] {
			resolved[name] = binding{masked: true}
		}
	}
	working := map[string]binding{}
	for k, v := range env {
		working[k] = v
	}
	for k, v := range resolved {
		working[k] = v
	}

	pending := map[string]bool{}
	for name := range byName {
		if !maskedNames[name] {
			pending[name] = true
		}
	}

	for len(pending) > 0 {
		progress := false
		for name := range pending {
			exprs := byName[name]
			var val string
			ok := true
			first := true
			for _, e := range exprs {
				v, eok := r.eval(e, working)
				if !eok {
					ok = false
					break
				}
				if first {
					val = v
					first = false
				} else if v != val {
					ok = false
					break
				}
			}
			if ok {
				working[name] = binding{val: val}
				delete(pending, name)
				progress = true
			}
		}
		if !progress {
			// Remaining pending names cannot be resolved: mask them.
			for name := range pending {
				working[name] = binding{masked: true}
			}
			break
		}
	}

	for name := range env {
		if v, ok := working[name]; ok {
			env[name] = v
		}
	}
	for name := range names {
		env[name] = working[name]
	}
}

// eval mirrors resolve's literal/ident/paren/+ cases but against a
// map[string]binding, treating a masked ident as a hard failure and an
// unknown ident as failure.
func (r *Resolver) eval(expr ast.Expr, env map[string]binding) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return resolve(e, nil)
	case *ast.Ident:
		b, ok := env[e.Name]
		if !ok || b.masked {
			return "", false
		}
		return b.val, true
	case *ast.ParenExpr:
		return r.eval(e.X, env)
	case *ast.BinaryExpr:
		if e.Op.String() != "+" {
			return "", false
		}
		l, lok := r.eval(e.X, env)
		if !lok {
			return "", false
		}
		rv, rok := r.eval(e.Y, env)
		if !rok {
			return "", false
		}
		return l + rv, true
	}
	return "", false
}
