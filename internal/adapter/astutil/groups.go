package astutil

import (
	"go/ast"
)

// GroupParams resolves router groups that are passed to a function instead of
// being assigned to a variable:
//
//	registerMFARoutes(private.Group("/auth/mfa"), c)
//
//	func registerMFARoutes(r *gin.RouterGroup, c *Container) {
//		r.POST("/enroll", ...)   // really POST /auth/mfa/enroll
//	}
//
// The group has no name at the call site, so a scanner that only records
// `v := router.Group("/x")` sees none of the prefix and documents every route
// inside the callee at its bare path — the whole group silently flattened.
// This resolves the argument to a prefix and returns it under the callee's
// parameter name.
//
// The result is keyed by the function the parameter belongs to, not by name
// alone. `r` is the name half the Go web code in existence gives its router,
// and a single global table would let one function's `r` rewrite another's:
// the prefix of a helper's parameter would attach itself to the root router in
// main, and every path in the document would gain a stray segment.
//
// prefixOf reports the prefix already known for a group identifier inside a
// given function (normally the adapter's own group table). It is consulted for
// the receiver of an `x.Group("/y")` argument and for an argument that is a
// plain group variable, so a prefix composes through both hops:
//
//	admin := private.Group("/admin")
//	registerBillingRoutes(admin.Group("/billing"))  // /admin/billing/...
//
// A function called from two places with different prefixes cannot have one
// answer; that is reported as a group-param diagnostic and the first prefix in
// source order wins, so the same source always produces the same document.
//
// out is filled in place rather than returned so that prefixOf can see the
// parameters resolved so far: a group that reaches a function through two hops
// is only resolvable once the first hop is known.
func GroupParams(files []*ast.File, res *Resolver, out map[*ast.FuncDecl]map[string]string, prefixOf func(name string, in *ast.FuncDecl) (string, bool), diags *Diagnostics, loc Locator) {
	GroupParamsIn(nil, files, res, out, prefixOf, diags, loc)
}

// GroupParamsIn is GroupParams with a package-aware index deciding which
// declaration a call names.
//
// Without one the callee is matched by bare name, and `Mount` is the name half
// the bounded contexts in a layered project give their route registration. The
// first one parsed then receives every other one's prefix, so a whole context's
// routes are documented under the wrong base path — or, when the winner takes
// no group at all, under none.
func GroupParamsIn(ix *FuncIndex, files []*ast.File, res *Resolver, out map[*ast.FuncDecl]map[string]string, prefixOf func(name string, in *ast.FuncDecl) (string, bool), diags *Diagnostics, loc Locator) {
	groupParamsWith(ix, files, res, GroupMethodCall, out, prefixOf, diags, loc)
}

// groupParamsWith is GroupParamsIn for a framework whose group calls are
// spelled some other way.
func groupParamsWith(ix *FuncIndex, files []*ast.File, res *Resolver, match GroupCall, out map[*ast.FuncDecl]map[string]string, prefixOf func(name string, in *ast.FuncDecl) (string, bool), diags *Diagnostics, loc Locator) {
	// Callees by name, methods included: without an index the name is the only
	// part resolvable, which is what a single-package project needs.
	funcs := FuncDecls(files)

	// Call sites are gathered once, in file order (files arrive sorted by
	// path), so the winner of a conflict does not depend on map iteration.
	type site struct {
		caller *ast.FuncDecl
		callee *ast.FuncDecl
		call   *ast.CallExpr
	}
	var sites []site
	for _, file := range files {
		for _, decl := range file.Decls {
			caller, ok := decl.(*ast.FuncDecl)
			if !ok || caller.Body == nil {
				continue
			}
			callerFile := file
			ast.Inspect(caller.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				var name string
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					name = fun.Name
				case *ast.SelectorExpr:
					name = fun.Sel.Name
				default:
					return true
				}
				callee := ix.Lookup(callerFile, caller, call.Fun)
				if callee == nil {
					callee = funcs[name]
				}
				if callee != nil && callee != caller {
					sites = append(sites, site{caller: caller, callee: callee, call: call})
				}
				return true
			})
		}
	}

	conflicted := map[*ast.FuncDecl]map[string]bool{}
	known := func(name string, in *ast.FuncDecl) (string, bool) {
		if p, ok := out[in][name]; ok {
			return p, true
		}
		if prefixOf == nil {
			return "", false
		}
		return prefixOf(name, in)
	}
	// A group can reach a function through another function
	// (registerAuthRoutes passes r.Group("/mfa") on to registerMFARoutes), so
	// one pass is not enough: iterate until nothing new resolves. Every pass
	// records at least one parameter or the loop ends, which bounds it.
	for {
		progress := false
		for _, s := range sites {
			params := paramNames(s.callee)
			for i, arg := range s.call.Args {
				if i >= len(params) || params[i] == "" || params[i] == "_" {
					continue
				}
				prefix, ok := groupArgPrefix(arg, res, match, diags, loc, s.caller, known)
				if !ok {
					continue
				}
				name := params[i]
				if prev, seen := out[s.callee][name]; seen {
					if prev != prefix && !conflicted[s.callee][name] {
						if conflicted[s.callee] == nil {
							conflicted[s.callee] = map[string]bool{}
						}
						conflicted[s.callee][name] = true
						diags.Add(loc.Position(arg.Pos()), "group-param",
							"parameter "+name+" of "+s.callee.Name.Name+" receives group "+
								prefix+" here and "+prev+" elsewhere; using "+prev)
					}
					continue
				}
				if out[s.callee] == nil {
					out[s.callee] = map[string]string{}
				}
				out[s.callee][name] = prefix
				progress = true
			}
		}
		if !progress {
			return
		}
	}
}

// groupArgPrefix resolves a call argument that is a router group to its full
// prefix: either a bare group variable, or an `x.Group("/y")` call whose own
// receiver may carry a prefix in turn. Both are read in the caller's scope.
func groupArgPrefix(arg ast.Expr, res *Resolver, match GroupCall, diags *Diagnostics, loc Locator, caller *ast.FuncDecl, known func(string, *ast.FuncDecl) (string, bool)) (string, bool) {
	switch e := arg.(type) {
	case *ast.Ident:
		return known(e.Name, caller)
	case *ast.CallExpr:
		recv, prefix, ok := match(e, res, diags, loc)
		if !ok {
			return "", false
		}
		// A receiver with no recorded prefix is the root router, which
		// contributes nothing — not a reason to give up on the argument.
		base := ""
		if recv != "" {
			base, _ = known(recv, caller)
		}
		return base + prefix, true
	}
	return "", false
}

// paramNames flattens a function's parameters to one name per position.
// Grouped parameters (a, b string) and unnamed ones both have to line up with
// argument indexes, so every position gets an entry even when it has no usable
// name.
func paramNames(fd *ast.FuncDecl) []string {
	if fd.Type == nil || fd.Type.Params == nil {
		return nil
	}
	var names []string
	for _, field := range fd.Type.Params.List {
		if len(field.Names) == 0 {
			names = append(names, "")
			continue
		}
		for _, id := range field.Names {
			names = append(names, id.Name)
		}
	}
	return names
}
