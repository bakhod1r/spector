# Function-local String Resolver Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve route paths built from function-local string vars (`base := "/v1"; r.Get(base+"/x", h)`) across all 8 router adapters, scope-aware, fixing the shadowing bug where a local var wrongly resolved to a same-named package const.

**Architecture:** Add a scope-aware `astutil.Resolver` that walks from a route-arg expression up its enclosing `FuncDecl`/`FuncLit` chain, layering per-function local string bindings over the package const map. Each adapter swaps its flat `consts map[string]string` for one `*Resolver`. A locally-declared but unresolvable name is *masked* (resolution fails → diagnostic) rather than falling back to a package const.

**Tech Stack:** Go, `go/ast`, `go/token`, existing `internal/adapter/astutil` package.

## Global Constraints

- Language floor: Go (module `github.com/bakhod1r/spector`). No new dependencies.
- `astutil.ResolveString(expr, consts)` and `astutil.StringConsts(files)` MUST be retained unchanged (other callers and tests depend on them; `ResolveString(expr, nil)` is a `StringLit` drop-in).
- `core.Adapter.Scan` signature is unchanged: `Scan(dir string) ([]core.Route, map[string]*core.Schema, []astutil.Diagnostic, error)`.
- Resolver MUST NOT panic on any AST shape: an unhandled expr returns `("", false)`.
- Masking rule (verbatim): a local var resolves to `V` **iff every** assignment to it in the enclosing function resolves to the same static string `V`; otherwise it is masked and `Resolve` returns `("", false)` — it MUST NOT fall back to a package const of the same name.
- Full suite `go test ./...` stays green after every task.
- TDD: write the failing test first, watch it fail, implement, watch it pass, commit.

---

### Task 1: `astutil.Resolver` core + unit tests

**Files:**
- Create: `internal/adapter/astutil/resolver.go`
- Test: `internal/adapter/astutil/resolver_test.go`

**Interfaces:**
- Consumes: existing `StringConsts(files []*ast.File) map[string]string`, `resolve(expr, consts)` (unexported, in `resolve.go`).
- Produces:
  - `func NewResolver(files []*ast.File) *Resolver`
  - `func (r *Resolver) Resolve(expr ast.Expr) (string, bool)`

**Design notes for the implementer:**
- `Resolver` holds: `pkg map[string]string` (= `StringConsts(files)`); `parent map[ast.Node]ast.Node` (built once by `ast.Inspect` over every file, recording each child's parent — use a stack, or set parent for every node visited via a walker that pushes/pops); `envs map[ast.Node]map[string]binding` memo keyed by the innermost enclosing func node.
- `binding` is `struct{ val string; masked bool }`.
- `Resolve(expr)`:
  1. Find the enclosing func chain by walking `parent` from `expr` up, collecting every `*ast.FuncDecl`/`*ast.FuncLit`, innermost first.
  2. If the chain is empty (expr not inside any func), fall back to `resolve(expr, r.pkg)`.
  3. Build/fetch the combined env for the innermost func (memoized): start from `r.pkg`; process funcs outermost→innermost, each adding its local bindings (inner names overwrite outer). Resolve within against the accumulating env.
  4. Resolve `expr` with a lookup that returns `("", false)` on a `masked` name and never consults `r.pkg` for a masked name.
- Local binding extraction for one func body (do **not** descend into nested `FuncLit` bodies — they get their own env):
  - `*ast.AssignStmt` (Tok `DEFINE` or `ASSIGN`) with 1:1 Lhs/Rhs: candidate `(identName, rhsExpr)` for each `*ast.Ident` Lhs.
  - `*ast.DeclStmt` → `*ast.GenDecl` (VAR) → `*ast.ValueSpec` with `len(Names)==len(Values)`: candidate per name.
  - A name assigned from a `range` variable, or whose candidate list is non-empty but any candidate fails to resolve **or** two candidates resolve to different strings → `masked`.
  - Resolve candidates to a fixed point (a local may depend on an earlier local: `base := "/v1"; sub := base+"/api"`).

- [ ] **Step 1: Write the failing tests**

```go
package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func parseSrc(t *testing.T, src string) []*ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return []*ast.File{f}
}

// firstArg returns the first argument of the first call to fnName in files.
func firstArg(t *testing.T, files []*ast.File, fnName string) ast.Expr {
	t.Helper()
	var found ast.Expr
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if found != nil {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == fnName && len(call.Args) > 0 {
				found = call.Args[0]
			}
			return true
		})
	}
	if found == nil {
		t.Fatalf("no call to %s found", fnName)
	}
	return found
}

func mustResolve(t *testing.T, src, fn, want string) {
	t.Helper()
	files := parseSrc(t, src)
	got, ok := NewResolver(files).Resolve(firstArg(t, files, fn))
	if !ok || got != want {
		t.Fatalf("Resolve = (%q,%v), want (%q,true)", got, ok, want)
	}
}

func mustMask(t *testing.T, src, fn string) {
	t.Helper()
	files := parseSrc(t, src)
	if got, ok := NewResolver(files).Resolve(firstArg(t, files, fn)); ok {
		t.Fatalf("Resolve = (%q,true), want masked (_,false)", got)
	}
}

func TestResolverLocalPrefix(t *testing.T) {
	mustResolve(t, `package p
func routes() { base := "/v1"; use(base+"/x") }
func use(string) {}`, "use", "/v1/x")
}

func TestResolverMultiHop(t *testing.T) {
	mustResolve(t, `package p
func routes() { base := "/v1"; sub := base+"/api"; use(sub+"/x") }
func use(string) {}`, "use", "/v1/api/x")
}

func TestResolverLocalShadowsPackageConst(t *testing.T) {
	mustResolve(t, `package p
const base = "/admin"
func routes() { base := "/v1"; use(base+"/x") }
func use(string) {}`, "use", "/v1/x")
}

func TestResolverMaskedShadowNoFallback(t *testing.T) {
	// The bug fix: a local shadow that is unresolvable must NOT resolve to the
	// package const of the same name.
	mustMask(t, `package p
const base = "/admin"
func routes() { base := ext(); use(base+"/x") }
func ext() string { return "" }
func use(string) {}`, "use")
}

func TestResolverReassignDiffers(t *testing.T) {
	mustMask(t, `package p
func routes(c bool) { base := "/v1"; if c { base = "/v2" }; use(base+"/x") }
func use(string) {}`, "use")
}

func TestResolverDynamicSources(t *testing.T) {
	mustMask(t, `package p
func routes(p string) { use(p+"/x") }
func use(string) {}`, "use")
	mustMask(t, `package p
func routes(m map[string]string) { base := m["k"]; use(base) }
func use(string) {}`, "use")
	mustMask(t, `package p
func routes(paths []string) { for _, base := range paths { use(base) } }
func use(string) {}`, "use")
}

func TestResolverNestedFuncLit(t *testing.T) {
	mustResolve(t, `package p
func routes() {
	base := "/v1"
	run(func() { sub := base+"/api"; use(sub+"/x") })
}
func run(func()) {}
func use(string) {}`, "use", "/v1/api/x")
}

func TestResolverNoScopeDropIn(t *testing.T) {
	// A bare literal outside any function resolves like StringLit.
	files := parseSrc(t, `package p
var x = "/lit"`)
	// Grab the literal directly.
	var lit ast.Expr
	ast.Inspect(files[0], func(n ast.Node) bool {
		if b, ok := n.(*ast.BasicLit); ok && b.Kind == token.STRING {
			lit = b
		}
		return true
	})
	if got, ok := NewResolver(files).Resolve(lit); !ok || got != "/lit" {
		t.Fatalf("Resolve(lit) = (%q,%v), want (/lit,true)", got, ok)
	}
}

func TestResolverPackageConstStillWorks(t *testing.T) {
	mustResolve(t, `package p
const base = "/v1"
func routes() { use(base+"/x") }
func use(string) {}`, "use", "/v1/x")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/adapter/astutil/ -run TestResolver -v`
Expected: FAIL — `undefined: NewResolver`.

- [ ] **Step 3: Implement `resolver.go`**

Implement `NewResolver`/`Resolve` per the design notes above. Reuse `resolve(expr, env)` for the leaf evaluation with a per-call env map. The masked lookup: build the combined env as `map[string]binding`; wrap it in a `map[string]string` view for `resolve` that omits masked names, but before calling `resolve`, if `expr`'s ident (or any ident it references) is masked, return `("", false)` — simplest correct approach is a dedicated recursive evaluator that consults `map[string]binding` directly (mirror `resolve`'s literal/ident/paren/`+` cases, treating a masked ident as a hard failure and an unknown ident as failure).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/adapter/astutil/ -run TestResolver -v`
Expected: PASS (all).

- [ ] **Step 5: Full astutil package + guard against regressions**

Run: `go test ./internal/adapter/astutil/`
Expected: PASS (existing `resolve_test.go` unaffected).

- [ ] **Step 6: Commit**

```bash
git add internal/adapter/astutil/resolver.go internal/adapter/astutil/resolver_test.go
git commit -m "feat(astutil): scope-aware Resolver for function-local string vars

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Adapter tasks (2–9): common pattern

Each adapter task performs the same mechanical swap and adds one testdata case.
The per-adapter specifics (exact call sites, route API) are given in full in
each task — do not cross-reference.

**Swap pattern (per adapter):**
1. Replace `consts := astutil.StringConsts(files)` with `res := astutil.NewResolver(files)`.
2. Replace every `astutil.ResolveString(<expr>, consts)` / `..., w.consts)` with `res.Resolve(<expr>)` / `w.res.Resolve(<expr>)`.
3. Change any struct field `consts map[string]string` to `res *astutil.Resolver`, and any helper parameter `consts map[string]string` to `res *astutil.Resolver`, updating call sites.

**Testdata pattern (per adapter):** add a small package (new dir under the adapter's `testdata/`, e.g. `testdata/localprefix/`) using that adapter's route API with a function-local prefix, then assert in the adapter's existing Scan test style that the composed route appears and produces **no** diagnostic.

---

### Task 2: chi adapter

**Files:**
- Modify: `internal/adapter/chi/chi.go` (`:62` StringConsts, `:80` field `consts`, `:122` ResolveString, `:152`/`:155` `groupBody` param+use)
- Create: `internal/adapter/chi/testdata/localprefix/routes.go`
- Test: `internal/adapter/chi/chi_test.go` (add case)

**Interfaces:**
- Consumes: `astutil.NewResolver`, `(*astutil.Resolver).Resolve`.

- [ ] **Step 1: Write failing testdata + test**

`internal/adapter/chi/testdata/localprefix/routes.go`:

```go
package localprefix

import "github.com/go-chi/chi/v5"

func Routes(r chi.Router) {
	base := "/v1"
	r.Get(base+"/categories", nil)
}
```

Add to `chi_test.go`:

```go
func TestChiLocalPrefix(t *testing.T) {
	a := &Adapter{}
	routes, _, diags, err := a.Scan("testdata/localprefix")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !hasRoute(routes, "GET", "/v1/categories") {
		t.Fatalf("route /v1/categories not resolved; got %v", routes)
	}
}
```

If `hasRoute` does not already exist in the test file, add:

```go
func hasRoute(rs []core.Route, method, path string) bool {
	for _, r := range rs {
		if r.Method == method && r.Path == path {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/adapter/chi/ -run TestChiLocalPrefix -v`
Expected: FAIL — a diagnostic is produced and the route is absent (local `base` unresolved).

- [ ] **Step 3: Apply the swap**

In `chi.go`: `:62` → `res := astutil.NewResolver(files)`; walker field `consts map[string]string` → `res *astutil.Resolver` (`:80`), constructor `w := &walker{... res: res, ...}`; `:122` → `path, ok := w.res.Resolve(call.Args[0])`; `groupBody` signature param `consts map[string]string` → `res *astutil.Resolver` and body `:155` → `res.Resolve(call.Args[0])`; update the `groupBody(...)` call to pass `w.res`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/adapter/chi/ -v`
Expected: PASS (new + existing).

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/chi/
git commit -m "feat(chi): resolve function-local route prefixes via Resolver

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: gin adapter

**Files:**
- Modify: `internal/adapter/gin/gin.go` (`:85` StringConsts, `:109` ResolveString, `:152` `collectGroups` param, `:172` ResolveString)
- Create: `internal/adapter/gin/testdata/localprefix/routes.go`
- Test: `internal/adapter/gin/gin_test.go`

- [ ] **Step 1: Write failing testdata + test**

`testdata/localprefix/routes.go`:

```go
package localprefix

import "github.com/gin-gonic/gin"

func Routes(r *gin.Engine) {
	base := "/v1"
	r.GET(base+"/categories", nil)
}
```

Test `TestGinLocalPrefix` mirrors Task 2 Step 1 (call `a.Scan("testdata/localprefix")`, assert no diagnostics and `hasRoute(routes,"GET","/v1/categories")`; add `hasRoute` helper if absent).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/adapter/gin/ -run TestGinLocalPrefix -v`
Expected: FAIL (route absent, diagnostic present).

- [ ] **Step 3: Apply the swap**

`:85` → `res := astutil.NewResolver(files)`; `:109` → `res.Resolve(call.Args[0])`; `collectGroups` param `consts map[string]string` → `res *astutil.Resolver` (`:152`) and its call site passes `res`; `:172` → `res.Resolve(call.Args[0])`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/adapter/gin/ -v` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/gin/
git commit -m "feat(gin): resolve function-local route prefixes via Resolver

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: echo adapter

**Files:**
- Modify: `internal/adapter/echo/echo.go` (`:64` StringConsts, `:76` `collectRoutes` param, `:90`/`:102`/`:116` ResolveString, `:190` `collectGroups` param, `:210` ResolveString)
- Create: `internal/adapter/echo/testdata/localprefix/routes.go`
- Test: `internal/adapter/echo/echo_test.go`

- [ ] **Step 1: Write failing testdata + test**

`testdata/localprefix/routes.go`:

```go
package localprefix

import "github.com/labstack/echo/v4"

func Routes(e *echo.Echo) {
	base := "/v1"
	e.GET(base+"/categories", nil)
}
```

Test `TestEchoLocalPrefix` mirrors Task 2 Step 1 (assert no diagnostics + `hasRoute(routes,"GET","/v1/categories")`).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/adapter/echo/ -run TestEchoLocalPrefix -v` — Expected: FAIL.

- [ ] **Step 3: Apply the swap**

`:64` → `res := astutil.NewResolver(files)`. Change `collectRoutes` param `consts map[string]string` → `res *astutil.Resolver` (`:76`) and `collectGroups` param likewise (`:190`), updating both call sites to pass `res`. Swap `ResolveString(..., consts)` → `res.Resolve(...)` at `:90`, `:102`, `:116`, `:210`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/adapter/echo/ -v` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/echo/
git commit -m "feat(echo): resolve function-local route prefixes via Resolver

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: fiber adapter

**Files:**
- Modify: `internal/adapter/fiber/fiber.go` (`:65` StringConsts, `:77` `collectRoutes` param, `:91`/`:115`/`:129` ResolveString, `:198` `collectGroups` param, `:218` ResolveString)
- Create: `internal/adapter/fiber/testdata/localprefix/routes.go`
- Test: `internal/adapter/fiber/fiber_test.go`

- [ ] **Step 1: Write failing testdata + test**

`testdata/localprefix/routes.go`:

```go
package localprefix

import "github.com/gofiber/fiber/v2"

func Routes(app *fiber.App) {
	base := "/v1"
	app.Get(base+"/categories", nil)
}
```

Test `TestFiberLocalPrefix` mirrors Task 2 Step 1 (assert no diagnostics + `hasRoute(routes,"GET","/v1/categories")`).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/adapter/fiber/ -run TestFiberLocalPrefix -v` — Expected: FAIL.

- [ ] **Step 3: Apply the swap**

`:65` → `res := astutil.NewResolver(files)`. `collectRoutes` param (`:77`) and `collectGroups` param (`:198`) `consts map[string]string` → `res *astutil.Resolver`; update call sites to pass `res`. Swap ResolveString → `res.Resolve` at `:91`, `:115`, `:129`, `:218`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/adapter/fiber/ -v` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/fiber/
git commit -m "feat(fiber): resolve function-local route prefixes via Resolver

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: gorillamux adapter

**Files:**
- Modify: `internal/adapter/gorillamux/gorillamux.go` (`:65` StringConsts, `:91` ResolveString on `reg.Args[0]`, `:189` `collectSubrouters` param, `:217` ResolveString on `inner.Args[0]`)
- Create: `internal/adapter/gorillamux/testdata/localprefix/routes.go`
- Test: `internal/adapter/gorillamux/gorillamux_test.go`

- [ ] **Step 1: Write failing testdata + test**

`testdata/localprefix/routes.go`:

```go
package localprefix

import "github.com/gorilla/mux"

func Routes(r *mux.Router) {
	base := "/v1"
	r.HandleFunc(base+"/categories", nil).Methods("GET")
}
```

Test `TestGorillamuxLocalPrefix` mirrors Task 2 Step 1 (assert no diagnostics + `hasRoute(routes,"GET","/v1/categories")`).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/adapter/gorillamux/ -run TestGorillamuxLocalPrefix -v` — Expected: FAIL.

- [ ] **Step 3: Apply the swap**

`:65` → `res := astutil.NewResolver(files)`; `:91` → `res.Resolve(reg.Args[0])`; `collectSubrouters` param (`:189`) → `res *astutil.Resolver` with call site passing `res`; `:217` → `res.Resolve(inner.Args[0])`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/adapter/gorillamux/ -v` — Expected: PASS (including existing `edge_test.go`).

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/gorillamux/
git commit -m "feat(gorillamux): resolve function-local route prefixes via Resolver

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 7: stdlib adapter

**Files:**
- Modify: `internal/adapter/stdlib/stdlib.go` (`:63` StringConsts, `:85` ResolveString, `:185` `collectMounts` param, `:197` ResolveString)
- Create: `internal/adapter/stdlib/testdata/localprefix/routes.go`
- Test: `internal/adapter/stdlib/stdlib_test.go`

- [ ] **Step 1: Write failing testdata + test**

`testdata/localprefix/routes.go` (Go 1.22 method-prefixed patterns; match the style already used in stdlib testdata — inspect a sibling testdata file first to confirm the mux var name and pattern form):

```go
package localprefix

import "net/http"

func Routes(mux *http.ServeMux) {
	base := "/v1"
	mux.HandleFunc("GET "+base+"/categories", nil)
}
```

Test `TestStdlibLocalPrefix`: assert no diagnostics and that a route with the composed path (`/v1/categories`, method `GET`) is present. If stdlib parses method+path from a single pattern string, assert `hasRoute(routes,"GET","/v1/categories")`; confirm the exact `core.Route` shape by reading an existing stdlib test assertion first.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/adapter/stdlib/ -run TestStdlibLocalPrefix -v` — Expected: FAIL.

- [ ] **Step 3: Apply the swap**

`:63` → `res := astutil.NewResolver(files)`; `:85` → `res.Resolve(call.Args[0])`; `collectMounts` param (`:185`) → `res *astutil.Resolver` with call site passing `res`; `:197` → `res.Resolve(call.Args[0])`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/adapter/stdlib/ -v` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/stdlib/
git commit -m "feat(stdlib): resolve function-local route prefixes via Resolver

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 8: httprouter adapter

**Files:**
- Modify: `internal/adapter/httprouter/httprouter.go` (`:66` StringConsts, `:80` field `consts`, `:109` `routingCall` param, `:111`/`:119` ResolveString)
- Create: `internal/adapter/httprouter/testdata/localprefix/routes.go`
- Test: `internal/adapter/httprouter/httprouter_test.go`

- [ ] **Step 1: Write failing testdata + test**

`testdata/localprefix/routes.go`:

```go
package localprefix

import "github.com/julienschmidt/httprouter"

func Routes(r *httprouter.Router) {
	base := "/v1"
	r.GET(base+"/categories", nil)
}
```

Test `TestHTTPRouterLocalPrefix` mirrors Task 2 Step 1 (assert no diagnostics + `hasRoute(routes,"GET","/v1/categories")`).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/adapter/httprouter/ -run TestHTTPRouterLocalPrefix -v` — Expected: FAIL.

- [ ] **Step 3: Apply the swap**

`:66` → `res := astutil.NewResolver(files)`; walker field `consts map[string]string` (`:80`) → `res *astutil.Resolver`; `routingCall` param `consts map[string]string` (`:109`) → `res *astutil.Resolver`; `:111`/`:119` → `res.Resolve(call.Args[0])` / `res.Resolve(call.Args[1])`; update the `routingCall(...)` call to pass the walker's `res`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/adapter/httprouter/ -v` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/httprouter/
git commit -m "feat(httprouter): resolve function-local route prefixes via Resolver

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 9: bunrouter adapter

**Files:**
- Modify: `internal/adapter/bunrouter/bunrouter.go` (`:65` StringConsts, `:79` field `consts`, `:108` ResolveString, `:140` `groupBody` param, `:143` ResolveString)
- Create: `internal/adapter/bunrouter/testdata/localprefix/routes.go`
- Test: `internal/adapter/bunrouter/bunrouter_test.go`

- [ ] **Step 1: Write failing testdata + test**

`testdata/localprefix/routes.go`:

```go
package localprefix

import "github.com/uptrace/bunrouter"

func Routes(r *bunrouter.Router) {
	base := "/v1"
	r.GET(base+"/categories", nil)
}
```

Test `TestBunrouterLocalPrefix` mirrors Task 2 Step 1 (assert no diagnostics + `hasRoute(routes,"GET","/v1/categories")`). Note: bunrouter handlers have a non-`http.HandlerFunc` signature — if `nil` fails to typecheck at parse time it is still fine (the adapter parses source, does not compile it); but match the handler shape used by existing bunrouter testdata if the test harness type-checks.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/adapter/bunrouter/ -run TestBunrouterLocalPrefix -v` — Expected: FAIL.

- [ ] **Step 3: Apply the swap**

`:65` → `res := astutil.NewResolver(files)`; walker field `consts` (`:79`) → `res *astutil.Resolver`; `:108` → `w.res.Resolve(call.Args[0])`; `groupBody` param (`:140`) → `res *astutil.Resolver` with call site passing `w.res`; `:143` → `res.Resolve(call.Args[0])`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/adapter/bunrouter/ -v` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/bunrouter/
git commit -m "feat(bunrouter): resolve function-local route prefixes via Resolver

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 10: Full suite + docs + caveat removal

**Files:**
- Modify: `README.md` (the static-route-resolution section documenting the package-level-only limitation and the shadowing caveat)
- Modify: `internal/adapter/astutil/resolve.go:12-16` doc comment on `StringConsts` if it claims package-level only (leave the function behaviour; update wording only if it now understates capability — the Resolver, not StringConsts, provides locals).

- [ ] **Step 1: Run the full suite**

Run: `go test ./...`
Expected: PASS (no FAIL, no panic).

- [ ] **Step 2: Update README**

Locate the passage stating that only package-level string const/var are resolved and the documented caveat that a local var shadowing a same-named package const resolves wrongly. Replace with: function-local string vars (`base := "/v1"`) are now resolved within their function, scope-aware (locals shadow package consts), and a local var that cannot be resolved statically is reported as a dynamic-route diagnostic rather than silently taking a package const's value. Keep the note that genuinely dynamic routes (loop/slice/map/func-return) still emit diagnostics.

- [ ] **Step 3: Commit**

```bash
git add README.md internal/adapter/astutil/resolve.go
git commit -m "docs: function-local route resolution; drop shadowing caveat

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- Resolver architecture (spec §Architecture) → Task 1. ✓
- Local env rule / masking (spec §Scope, §Local env rule) → Task 1 tests 4–6, 2. ✓
- Shadowing bug fix (spec §Shadowing bug fix) → Task 1 `TestResolverMaskedShadowNoFallback`, Task 1 `TestResolverLocalShadowsPackageConst`. ✓
- 8-adapter integration (spec §Adapter integration) → Tasks 2–9 (chi, gin, echo, fiber, gorillamux, stdlib, httprouter, bunrouter). ✓
- Retain `ResolveString`/`StringConsts` (spec §Architecture, Global Constraints) → Task 1 keeps `resolve.go`; `TestResolverNoScopeDropIn`. ✓
- Docs/caveat removal (spec implied by fixing the documented caveat) → Task 10. ✓
- Full suite green (spec §Testing) → Task 10 Step 1. ✓

**Placeholder scan:** No "TBD"/"handle edge cases"/"similar to Task N". Each adapter task repeats its concrete swap sites and testdata. stdlib/bunrouter tasks flag "inspect the sibling testdata for exact shape" — this is a real instruction to verify a parser-specific detail, not a placeholder for missing code.

**Type consistency:** `NewResolver(files []*ast.File) *Resolver` and `(*Resolver).Resolve(ast.Expr) (string, bool)` are used identically in every adapter task. Field/param renames are uniformly `consts map[string]string` → `res *astutil.Resolver`. `hasRoute(rs []core.Route, method, path string) bool` defined once, reused. ✓
