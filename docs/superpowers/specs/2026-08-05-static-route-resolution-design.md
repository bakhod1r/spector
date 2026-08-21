# Static Route Resolution (const/var/concat) + Dynamic-Route Diagnostics — Design

Date: 2026-08-05
Status: Approved (design)
Feature: 2a of the "dynamic route inference" backlog item (2b = proxy-learn pairing, separate spec).

## Problem

Route detection is AST-only and recognises a route path/prefix **only when the
argument is a string literal**. Every adapter resolves the path via
`astutil.StringLit(call.Args[0])` (e.g. [gin.go:107](internal/adapter/gin/gin.go),
and the group-prefix site [gin.go:169]) and silently `return`s when it is not a
`*ast.BasicLit`. So these real-world patterns produce **no documentation and no
signal**:

```go
const userPath = "/users/:id"
r.GET(userPath, h)              // skipped

base := "/api/v1"
r.GET(base+"/users", h)         // skipped

api := r.Group(apiPrefix)       // group prefix skipped → every child route wrong
```

Two gaps: (1) values that are statically knowable (constants, simple string
vars, concatenations) are thrown away; (2) genuinely dynamic routes (a path that
is a range variable, a slice element, a function return) vanish with no diagnostic,
so the user cannot even tell coverage is incomplete.

## Goal

1. Resolve route paths and group prefixes from **package-level string `const`
   and `var` declarations, identifiers referencing them, and `+`
   concatenations** of resolvable parts — across all eight router adapters —
   with zero new annotations.
2. Emit a **diagnostic** (file:line + reason) for each route/prefix site whose
   argument cannot be statically resolved, surfaced to stderr, so the user sees
   exactly what AST scanning missed. This diagnostic stream is also the input
   surface that feature 2b (`-proxy-learn` pairing) will later fill from live
   traffic.

## Non-goals

- No cross-package resolution (only consts/vars declared in the scanned source
  tree's packages; an imported `otherpkg.Path` is treated as unresolved →
  diagnostic).
- No full type checking / SSA. Value-flow beyond package-level decl + local
  same-file assignment + concat is out of scope (→ diagnostic).
- No resolution of runtime-constructed routes (range vars, slice/map elements,
  function returns) — those are the diagnostic case and 2b's target.
- No change to matching (`internal/route`), export formats, or the console.

## Design

### 1. String-value resolver (`internal/adapter/astutil`)

New, shared by every adapter so behaviour is uniform:

```go
// StringConsts indexes package-level string const and var declarations across
// the given files by name → value. Only entries whose initializer resolves to a
// static string (a string literal, another indexed name, or a concatenation of
// those) are included; a var whose initializer is dynamic is omitted.
func StringConsts(files []*ast.File) map[string]string

// ResolveString statically evaluates expr to a string using consts:
//   - *ast.BasicLit (STRING)            → its unquoted value
//   - *ast.Ident                        → consts[name] if present
//   - *ast.BinaryExpr with token.ADD    → ResolveString(X)+ResolveString(Y)
//   - *ast.ParenExpr                    → ResolveString(inner)
// Returns ("", false) for anything else (including a name not in consts).
func ResolveString(expr ast.Expr, consts map[string]string) (string, bool)
```

Notes:
- `StringConsts` handles forward references and const/var chains by iterating to
  a fixed point (or two passes): a `var b = a + "/x"` where `a` is another
  package-level const must resolve regardless of declaration order.
- Both `const` (`token.CONST`) and `var` (`token.VAR`) `GenDecl` specs are read.
  A grouped `const ( a = "..."; b = a+"/x" )` is supported.
- `StringLit` stays as-is (still used where only a literal is meaningful, e.g.
  struct-tag parsing); `ResolveString(expr, nil)` degenerates to literal-only,
  so it is a safe drop-in.

### 2. Diagnostics collector (`internal/adapter/astutil`)

```go
type Diagnostic struct {
    Pos    token.Position // file:line:col of the unresolved argument
    Kind   string         // "route" | "group-prefix"
    Reason string         // short: e.g. "path is not a static string (range variable)"
}

type Diagnostics struct { /* holds []Diagnostic; append-only; concurrency not required */ }

func (d *Diagnostics) Add(pos token.Position, kind, reason string)
func (d *Diagnostics) List() []Diagnostic   // in source order (sorted by file then line)
```

The `Reason` is derived from the unresolved expr's node type (a small helper
`describeExpr(ast.Expr) string` → "range variable", "function call result",
"slice/map element", "identifier from another package", "non-literal
expression"). Best-effort; never fails.

### 3. Threading through adapters

The eight adapters share the same shape: `Scan(dir)` parses files, builds
`groups`, then walks call expressions calling `astutil.StringLit(call.Args[0])`
at the route site and (for grouped routers) at the group-prefix site.

Changes per adapter:
- Build `consts := astutil.StringConsts(files)` once in `Scan`, alongside the
  existing `handlers`/`groups` collection.
- At each route site, replace
  `path, ok := astutil.StringLit(call.Args[0])` with
  `path, ok := astutil.ResolveString(call.Args[0], consts)`; on `!ok`, call
  `diags.Add(loc.Position(call.Args[0].Pos()), "route", describeExpr(call.Args[0]))`
  before `return true`.
- At each group-prefix site (`collectGroups`/`groupDef`), same swap with kind
  `"group-prefix"`.
- Thread a `*astutil.Diagnostics` through `collectRoutes`/`collectGroups` the
  same way `loc`, `mw`, `index` are already threaded.

`loc astutil.Locator` already wraps a `*token.FileSet`; add a
`Position(token.Pos) token.Position` method to `Locator` if one is not already
exposed, so adapters can turn a `Pos` into a `file:line`.

### 4. Surfacing the diagnostics

The `Adapter.Scan` interface today is:

```go
Scan(dir string) ([]core.Route, map[string]*core.Schema, error)
```

**Decision (the main design point):** add diagnostics as a fourth return value
rather than a side channel, keeping resolution results explicit and testable:

```go
Scan(dir string) ([]core.Route, map[string]*core.Schema, []astutil.Diagnostic, error)
```

- All eight adapters updated to return `diags.List()`.
- The single dispatch/caller (in `spector.go` / `cmd/spector/main.go`, wherever
  `Adapter.Scan` is invoked — grep `.Scan(`) collects the diagnostics.
- The CLI prints them to **stderr** (never stdout, which may carry the spec),
  one line each:
  `spector: <file>:<line>: dynamic <kind>, cannot infer path (<reason>)`
  plus a trailing summary count. A new flag `-strict-routes` (default off) makes
  any diagnostic a non-zero exit; default is warn-and-continue so existing runs
  are unaffected except for added stderr lines.
- The library entry point (`spector.go` wrapper) exposes the diagnostics on its
  result so programmatic callers (and 2b) can consume them without parsing
  stderr.

This is a breaking change to the `Adapter` interface, which is internal
(`internal/adapter`), so no external consumer is affected.

### 5. Interaction with existing behaviour

- A route that *was* resolvable as a literal stays byte-identical; `ResolveString`
  returns the same value for a `BasicLit`. Existing golden/testdata output must
  not change except where a previously-skipped const/var/concat route now
  appears (those are net-new routes in the testdata that exercises them).
- Group prefixes that were literal are unchanged; a previously-skipped dynamic
  prefix now either resolves (correct child paths) or produces one diagnostic.

## Testing

- `astutil` unit tests (new `astutil_resolve_test.go`):
  - `StringConsts`: const literal; var literal; const referencing const; var
    referencing const with concat; grouped const block; forward reference;
    dynamic var initializer omitted; non-string decl ignored.
  - `ResolveString`: literal; ident hit; ident miss → false; `a+"/x"`;
    `"x"+"/"+b`; parenthesised; unresolved kinds → false.
  - `describeExpr`: range var, call, index expr, selector (other package) map to
    stable reason strings.
- Per-adapter tests: add testdata exercising a const path, a var-prefix group,
  and a concat path for **at least gin, chi, echo, stdlib** (the representative
  spread); assert the routes now appear with correct paths. Add one genuinely
  dynamic route (range var) and assert exactly one diagnostic with the right
  file:line and kind.
- CLI test: a fixture with one dynamic route → stderr contains the diagnostic
  line; exit 0 by default; exit non-zero under `-strict-routes`.
- Full suite green: `go test ./...`.

## Workflow

Branch, TDD, `go test ./...`, commit, merge `--no-ff` on approval. 2a ships
first; 2b (`-proxy-learn` fills the diagnostic gaps from live traffic) is a
separate spec → plan → implementation once 2a lands.
