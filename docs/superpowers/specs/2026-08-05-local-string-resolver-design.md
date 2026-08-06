# Function-local string resolution — design

## Problem

`astutil.StringConsts` resolves route paths and group prefixes from
**package-level** string `const`/`var` (plus `+` concatenation) across all 8
adapters. Two gaps remain:

1. **No function-local vars.** A common pattern emits a diagnostic instead of
   resolving:

   ```go
   func routes(r chi.Router) {
       base := "/v1"
       r.Get(base+"/categories", h) // → "route" diagnostic, path lost
   }
   ```

2. **Not scope-aware (a correctness bug).** The package map is consulted inside
   functions regardless of shadowing. A local var that shadows a same-named
   package const wrongly resolves to the **package** value:

   ```go
   const base = "/admin"
   func routes(r chi.Router) {
       base := "/v1"
       r.Get(base+"/x", h) // wrongly resolves to "/admin/x"
   }
   ```

## Scope (approved: "A" — deterministic intra-function)

Resolve a local string var to value `V` **iff every** assignment to it in the
enclosing function resolves to the same static string `V`. Otherwise the name is
**masked**: resolution fails and the route emits the existing diagnostic — it
does **not** fall back to a package const of the same name (this fixes bug 2).

- Considers **all** assignments at any block depth (`:=`, `var x = …`, `x = …`)
  — the "full intra-function" part.
- A var sourced from anything dynamic (function call, parameter, slice/map index,
  range/loop variable), or assigned differing constant values, is masked.
- No control-flow graph. "Every assignment resolves to the same `V`" is a
  conservative, deterministic rule that covers `base := "/v1"` and stays safe on
  reassignment. Genuinely dynamic routes remain the job of proxy-learn (2b).

Explicitly **out of scope**: resolving loop/collection-driven routes to multiple
paths (that is 2b), and cross-function/closure data flow beyond lexical nesting.

## Architecture — `astutil.Resolver`

Today each adapter calls `astutil.ResolveString(expr, pkgConsts)` with a flat
package map and no notion of where `expr` sits. Introduce a shared resolver that
carries scope.

```go
// NewResolver builds a scope-aware resolver over a package's files.
func NewResolver(files []*ast.File) *Resolver

// Resolve evaluates expr to a static string, honouring the lexical scope of the
// function (and nested FuncLits) that encloses expr: inner locals shadow outer
// locals shadow package consts. A locally-declared but unresolvable name is
// masked (returns "", false) rather than resolving to a package const.
func (r *Resolver) Resolve(expr ast.Expr) (string, bool)
```

Internals:

- **Package consts:** reuse `StringConsts(files)`.
- **Parent links:** one pass per file building `map[ast.Node]ast.Node` so any
  expr can walk up to its enclosing `*ast.FuncDecl` / `*ast.FuncLit` chain
  (`astutil.parents`, unexported).
- **Per-function local env (memoized):** for a given func node, scan its body
  for string assignments and produce `map[string]binding` where `binding` is
  either a resolved value or `masked`. Nested `FuncLit` scopes layer over their
  enclosing func's env.
- `Resolve(expr)`: find the enclosing func chain (innermost out); consult local
  envs then package consts; a `masked` hit short-circuits to `("", false)`.

The existing pure helper stays for callers that have no scope:

- `ResolveString(expr, consts)` is retained unchanged (drop-in, `consts=nil`
  behaves like `StringLit`). `StringConsts` is retained (Resolver uses it).

## Adapter integration

Each adapter's `Scan` builds one `Resolver` from its parsed files and threads it
through the walker in place of the raw `consts` map:

- `walker.consts map[string]string` → `walker.res *astutil.Resolver`.
- Every `astutil.ResolveString(arg, w.consts)` → `w.res.Resolve(arg)`.
- Group/prefix helpers (`groupBody`, echo `Group`, gin `RouterGroup`, etc.) that
  currently take `consts` take the `*Resolver` instead.

8 adapters: gin, chi, echo, fiber, gorillamux, stdlib, httprouter, bunrouter.
The diagnostic call sites (`diags.Add(..., DescribeExpr(arg))`) are unchanged —
a masked name simply reaches them as before.

## Data flow

`Scan(dir)` → parse files → `res := NewResolver(files)` → walk routes →
`res.Resolve(pathArg)` at each site → resolved `core.Route.Path` or a
`core.Diagnostic`. No change to `core.Document` or the `Adapter` interface
signature (`Scan` already returns `[]core.Diagnostic`).

## Error handling

- Resolver never panics on malformed AST: an unhandled expr shape returns
  `("", false)`, exactly like the current `resolve`.
- A masked var yields a diagnostic, never a wrong path — the safe failure mode.
- Missing parent (expr not under any func — e.g. a package-level route table)
  falls back to package consts only, preserving today's behaviour.

## Testing (TDD)

New `astutil` unit tests (`resolver_test.go`):

1. `base := "/v1"; use(base+"/x")` → `/v1/x`.
2. Multi-hop local: `base := "/v1"; sub := base+"/api"; use(sub+"/x")` → `/v1/api/x`.
3. Shadowing: local `base := "/v1"` over `const base = "/admin"` → local wins.
4. **Masking bug fix:** local `base := someFn()` shadowing `const base = "/admin"`
   → masked (`ok=false`), not `/admin`.
5. Reassignment to differing literals → masked.
6. Dynamic sources (call/param/index/range var) → masked.
7. Nested `FuncLit` scope: inner local shadows outer local.
8. No-scope drop-in: `ResolveString(lit, nil)` unchanged.

Per-adapter: one testdata case each with a `base := "…"` prefix that now
resolves, asserting the route appears with the composed path and **no**
diagnostic. Keep an existing genuinely-dynamic case asserting the diagnostic
still fires. Full suite (`go test ./...`) stays green.

## Non-goals / risk

- Moderate cross-adapter refactor (swap `consts` → `Resolver`), but mechanical
  and each adapter has tests to catch regressions.
- No public API break: `Config`, `Adapter`, `Document` unchanged;
  `ResolveString`/`StringConsts` retained.
