package astutil

import "go/ast"

// GroupResolver is the full path prefix a router variable carries, for the
// frameworks that spell a group as `v := router.Group("/prefix")` — gin, echo
// and fiber all do.
//
// It composes three facts that a project needs all of and that each adapter
// used to implement none, one or two of:
//
//   - a group assigned to a variable, nested through its receiver;
//   - a group handed to a function as an argument, which is how registration
//     is split up once a service outgrows one file;
//   - the function each name belongs to, because `r`, `api` and `group` are
//     what half the router variables in Go are called, and one shared table
//     lets one package's prefix rewrite another's.
type GroupResolver struct {
	table  groupTable
	params map[*ast.FuncDecl]map[string]string
}

type groupDef struct {
	recv   string
	prefix string
	// fn is the function the assignment appears in, so the receiver name is
	// resolved in the scope that gave it meaning.
	fn *ast.FuncDecl
}

// groupTable is every group assignment, keyed by the function it was written
// in and then by the variable's name.
type groupTable map[*ast.FuncDecl]map[string]groupDef

func (t groupTable) put(fn *ast.FuncDecl, name string, def groupDef) {
	if t[fn] == nil {
		t[fn] = map[string]groupDef{}
	}
	if _, taken := t[fn][name]; !taken {
		t[fn][name] = def
	}
}

// lookup finds a group by name in the function that used it. A package-level
// router is stored under the nil function, which is also where a lookup falls
// back to.
func (t groupTable) lookup(fn *ast.FuncDecl, name string) (groupDef, bool) {
	if g, ok := t[fn][name]; ok {
		return g, true
	}
	g, ok := t[nil][name]
	return g, ok
}

// GroupCall reads the expression a router group is assigned from and reports
// the variable it derives from and the path it adds.
//
// Frameworks spell this differently — gin, echo and fiber all say
// router.Group("/x"), gorilla/mux says router.PathPrefix("/x").Subrouter() —
// but everything downstream is identical, so the spelling is the only part an
// adapter supplies.
type GroupCall func(call *ast.CallExpr, res *Resolver, diags *Diagnostics, loc Locator) (recv, prefix string, ok bool)

// GroupMethodCall matches router.Group("/x"), the spelling gin, echo and fiber
// share.
func GroupMethodCall(call *ast.CallExpr, res *Resolver, diags *Diagnostics, loc Locator) (recv, prefix string, ok bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != "Group" || len(call.Args) < 1 {
		return "", "", false
	}
	p, resolved := res.Resolve(call.Args[0])
	if !resolved {
		diags.Add(loc.Position(call.Args[0].Pos()), "group-prefix", DescribeExpr(call.Args[0]))
		return "", "", false
	}
	if id, isIdent := sel.X.(*ast.Ident); isIdent {
		recv = id.Name
	}
	return recv, p, true
}

// SubrouterCall matches router.PathPrefix("/x").Subrouter(), gorilla/mux's
// spelling of the same thing.
func SubrouterCall(call *ast.CallExpr, res *Resolver, diags *Diagnostics, loc Locator) (recv, prefix string, ok bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != "Subrouter" {
		return "", "", false
	}
	inner, isCall := sel.X.(*ast.CallExpr)
	if !isCall {
		return "", "", false
	}
	innerSel, isSel := inner.Fun.(*ast.SelectorExpr)
	if !isSel || innerSel.Sel.Name != "PathPrefix" || len(inner.Args) < 1 {
		return "", "", false
	}
	p, resolved := res.Resolve(inner.Args[0])
	if !resolved {
		diags.Add(loc.Position(inner.Args[0].Pos()), "group-prefix", DescribeExpr(inner.Args[0]))
		return "", "", false
	}
	if id, isIdent := innerSel.X.(*ast.Ident); isIdent {
		recv = id.Name
	}
	return recv, p, true
}

// NewGroupResolver reads every group assignment and every group passed as an
// argument, for the router.Group("/x") spelling.
//
// scope may be nil; the callee of a hand-off is then matched by bare name,
// which is what a single-package project has anyway.
func NewGroupResolver(scope *Scope, files []*ast.File, res *Resolver, diags *Diagnostics, loc Locator) *GroupResolver {
	return NewGroupResolverWith(scope, files, res, diags, loc, GroupMethodCall)
}

// NewGroupResolverWith is NewGroupResolver for a framework that spells a group
// some other way.
func NewGroupResolverWith(scope *Scope, files []*ast.File, res *Resolver, diags *Diagnostics, loc Locator, match GroupCall) *GroupResolver {
	g := &GroupResolver{
		table:  groupTable{},
		params: map[*ast.FuncDecl]map[string]string{},
	}
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
			recv, prefix, ok := match(call, res, diags, loc)
			if !ok {
				return true
			}
			fn := res.EnclosingFunc(as)
			g.table.put(fn, lhs.Name, groupDef{recv: recv, prefix: prefix, fn: fn})
			return true
		})
	}

	var index *FuncIndex
	if scope != nil {
		index = scope.Index
	}
	groupParamsWith(index, files, res, match, g.params, func(name string, in *ast.FuncDecl) (string, bool) {
		if _, ok := g.params[in][name]; !ok {
			if _, ok := g.table.lookup(in, name); !ok {
				return "", false
			}
		}
		return g.Prefix(name, in), true
	}, diags, loc)
	return g
}

// Prefix is the path a router variable carries, seen from inside fn.
func (g *GroupResolver) Prefix(name string, fn *ast.FuncDecl) string {
	if g == nil {
		return ""
	}
	type step struct {
		fn   *ast.FuncDecl
		name string
	}
	seen := map[step]bool{}
	prefix := ""
	for {
		if p, ok := g.params[fn][name]; ok {
			// A parameter's prefix is already composed at the call site, so
			// there is nothing above it to walk to.
			return p + prefix
		}
		def, ok := g.table.lookup(fn, name)
		if !ok || seen[step{fn, name}] {
			return prefix
		}
		seen[step{fn, name}] = true
		prefix = def.prefix + prefix
		name = def.recv
		fn = def.fn
	}
}
