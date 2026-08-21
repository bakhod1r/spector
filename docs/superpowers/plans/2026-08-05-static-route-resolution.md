# Static Route Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve route paths and group prefixes from package-level string `const`/`var` declarations, identifiers, and `+` concatenations across all eight router adapters, and emit a diagnostic for every route site whose path cannot be statically resolved.

**Architecture:** A shared resolver in `internal/adapter/astutil` (`StringConsts` builds a name→value table; `ResolveString` statically evaluates literal/ident/concat/paren expressions) replaces the literal-only `StringLit` at each adapter's route-path and group-prefix site. A `Diagnostics` collector records unresolved sites with `file:line` + reason. The `Adapter.Scan` interface gains a fourth return value carrying the diagnostics, which the CLI prints to stderr (with an opt-in `-strict-routes` non-zero exit).

**Tech Stack:** Go, `go/ast`, `go/token`, `go/parser`; internal packages `internal/adapter/*`, `internal/core`, `spector.go`, `cmd/spector`.

## Global Constraints

- Module path `github.com/bakhod1r/spector`.
- Zero new external dependencies; zero new required annotations (Spector's promise: no source changes needed to be documented).
- `StringLit` (`astutil.go:18`) stays as-is; `ResolveString(expr, nil)` must be a behaviour-preserving drop-in for it (literal → same value, everything else → false).
- Only package-level consts/vars in the scanned tree are resolved; no cross-package, no SSA, no type checking. Unresolved → diagnostic, never a crash.
- Existing golden/testdata output must not change except for net-new routes in testdata specifically added to exercise resolution.
- Diagnostics go to **stderr** only (stdout may carry the generated spec). Default run stays exit 0 (warn-and-continue); `-strict-routes` makes any diagnostic exit non-zero.
- The `Adapter` interface is internal (`internal/adapter`); changing its signature is allowed.

## File Structure

- `internal/adapter/astutil/resolve.go` — new: `StringConsts`, `ResolveString`, `Diagnostic`, `Diagnostics`, `DescribeExpr` (exported — adapters in sibling packages call it). (Kept separate from the large `astutil.go` for focus.)
- `internal/adapter/astutil/resolve_test.go` — new: unit tests for the above.
- `internal/adapter/astutil/astutil.go` — modify: add `Locator.Position(token.Pos) token.Position`.
- `internal/adapter/{gin,chi,echo,fiber,gorillamux,httprouter,bunrouter,stdlib}/*.go` — modify: build consts + diagnostics, swap route-path/group-prefix sites to `ResolveString`, return diagnostics.
- Each adapter's `testdata/` — add fixtures exercising const/var/concat routes and one genuinely dynamic route.
- `spector.go` — modify: both `adapterFor(cfg).Scan(cfg.Dir)` call sites (lines 248, 530) to accept the 4th return; expose diagnostics on the library result.
- `cmd/spector/main.go` — modify: print diagnostics to stderr; add `-strict-routes` flag.
- `README.md` — modify: document static resolution + diagnostics.

## Adapter route-path / group-prefix site map

These are the exact sites to convert (verb/method-name/param-name `StringLit` calls are intentionally left literal-only):

| Adapter | Route-path sites (file:line, arg) | Group-prefix sites |
|---|---|---|
| gin | 107 (arg0) | 169 (arg0) |
| chi | 118 (arg0) | 150 (arg0, subrouter) |
| echo | 88 (Any, arg0), 98 (Match, arg1), 110 (arg0) | 203 (arg0) |
| fiber | 89 (All, arg0), 111 (Add, arg1), 124 (arg0) | 212 (arg0) |
| gorillamux | 89 (arg0) | 214 (arg0) |
| httprouter | 107 (arg0), 114 (Handle, arg1) | — (no groups) |
| bunrouter | 104 (arg0) | 138 (arg0) |
| stdlib | 83 (arg0) | 194 (arg0) |

Leave literal-only (do NOT convert): echo:160, fiber:100, gorillamux:162, httprouter:113 (these are param names / HTTP verbs), and `astutil.go:586` (`addParam`).

---

### Task 1: String-value resolver in astutil

**Files:**
- Create: `internal/adapter/astutil/resolve.go`
- Test: `internal/adapter/astutil/resolve_test.go`

**Interfaces:**
- Produces:
  - `func StringConsts(files []*ast.File) map[string]string`
  - `func ResolveString(expr ast.Expr, consts map[string]string) (string, bool)`

- [ ] **Step 1: Write the failing test**

Create `internal/adapter/astutil/resolve_test.go`:

```go
package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// parseFile parses a single Go source string into an *ast.File.
func parseFile(t *testing.T, src string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

func TestStringConsts(t *testing.T) {
	src := `package p
const A = "/a"
const (
	B = "/b"
	C = B + "/c"
)
var D = "/d"
var E = A + "/e"
var F = someCall()
const G = 42
`
	f := parseFile(t, src)
	consts := StringConsts([]*ast.File{f})
	want := map[string]string{"A": "/a", "B": "/b", "C": "/b/c", "D": "/d", "E": "/a/e"}
	for k, v := range want {
		if consts[k] != v {
			t.Errorf("consts[%q] = %q, want %q", k, consts[k], v)
		}
	}
	if _, ok := consts["F"]; ok {
		t.Errorf("F has a dynamic initializer and must be omitted")
	}
	if _, ok := consts["G"]; ok {
		t.Errorf("G is not a string and must be omitted")
	}
}

func TestResolveString(t *testing.T) {
	consts := map[string]string{"base": "/api/v1", "u": "/users"}
	f := parseFile(t, `package p
var _ = ""+base+u+"/x"
`)
	// Extract the RHS expression of the var.
	var expr ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok && len(vs.Values) == 1 {
			expr = vs.Values[0]
		}
		return true
	})
	got, ok := ResolveString(expr, consts)
	if !ok || got != "/api/v1/users/x" {
		t.Fatalf("ResolveString = %q,%v; want /api/v1/users/x,true", got, ok)
	}
}

// A bare string literal must resolve identically to StringLit — this is the
// drop-in guarantee that keeps existing route output byte-identical.
func TestResolveStringLiteralDropIn(t *testing.T) {
	f := parseFile(t, `package p
var _ = "/lit"
`)
	var expr ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok && len(vs.Values) == 1 {
			expr = vs.Values[0]
		}
		return true
	})
	got, ok := ResolveString(expr, nil)
	if !ok || got != "/lit" {
		t.Fatalf("ResolveString(lit,nil) = %q,%v; want /lit,true", got, ok)
	}
}

func TestResolveStringUnresolved(t *testing.T) {
	f := parseFile(t, `package p
func g(paths []string){ _ = paths[0] }
`)
	var expr ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		if ix, ok := n.(*ast.IndexExpr); ok {
			expr = ix
		}
		return true
	})
	if _, ok := ResolveString(expr, map[string]string{}); ok {
		t.Fatalf("index expression must not resolve")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapter/astutil/ -run 'TestStringConsts|TestResolveString' -v`
Expected: FAIL — `StringConsts`/`ResolveString` undefined.

- [ ] **Step 3: Implement the resolver**

Create `internal/adapter/astutil/resolve.go`:

```go
package astutil

import (
	"go/ast"
	"go/token"
	"strconv"
)

// StringConsts indexes package-level string const and var declarations by name.
// Only entries whose initializer statically resolves to a string (a string
// literal, another indexed name, or a + concatenation of those) are included.
// It iterates to a fixed point so declaration order and forward references do
// not matter.
func StringConsts(files []*ast.File) map[string]string {
	// Collect candidate name→initializer expressions from top-level const/var.
	type cand struct {
		name string
		expr ast.Expr
	}
	var cands []cand
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue // skip iota / typed-without-value / multi-assign shapes
				}
				for i, name := range vs.Names {
					cands = append(cands, cand{name: name.Name, expr: vs.Values[i]})
				}
			}
		}
	}
	out := map[string]string{}
	// Fixed point: each pass resolves any candidate whose deps are already known.
	for {
		progress := false
		for _, c := range cands {
			if _, done := out[c.name]; done {
				continue
			}
			if v, ok := resolve(c.expr, out); ok {
				out[c.name] = v
				progress = true
			}
		}
		if !progress {
			return out
		}
	}
}

// ResolveString statically evaluates expr to a string using consts:
//   - string literal            → its unquoted value
//   - identifier                → consts[name] if present
//   - X + Y (token.ADD)         → ResolveString(X) + ResolveString(Y)
//   - (inner)                   → ResolveString(inner)
// Anything else, or an identifier not in consts, returns ("", false).
// ResolveString(expr, nil) is a behaviour-preserving drop-in for StringLit.
func ResolveString(expr ast.Expr, consts map[string]string) (string, bool) {
	return resolve(expr, consts)
}

func resolve(expr ast.Expr, consts map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.Ident:
		v, ok := consts[e.Name]
		return v, ok
	case *ast.ParenExpr:
		return resolve(e.X, consts)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, lok := resolve(e.X, consts)
		if !lok {
			return "", false
		}
		r, rok := resolve(e.Y, consts)
		if !rok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/adapter/astutil/ -run 'TestStringConsts|TestResolveString' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/astutil/resolve.go internal/adapter/astutil/resolve_test.go
git commit -m "feat(astutil): resolve string consts/vars/concats for route paths"
```

---

### Task 2: Diagnostics collector + DescribeExpr + Locator.Position

**Files:**
- Modify: `internal/adapter/astutil/resolve.go`
- Modify: `internal/adapter/astutil/astutil.go` (add `Locator.Position`)
- Test: `internal/adapter/astutil/resolve_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 beyond the same package.
- Produces:
  - `type Diagnostic struct { Pos token.Position; Kind, Reason string }`
  - `type Diagnostics struct { ... }` with `func (d *Diagnostics) Add(pos token.Position, kind, reason string)` and `func (d *Diagnostics) List() []Diagnostic`
  - `func DescribeExpr(ast.Expr) string` (exported — adapters call it)
  - `func (l Locator) Position(pos token.Pos) token.Position`

- [ ] **Step 1: Write the failing test**

Append to `internal/adapter/astutil/resolve_test.go`:

```go
func TestDiagnosticsSorted(t *testing.T) {
	var d Diagnostics
	d.Add(token.Position{Filename: "b.go", Line: 3}, "route", "range variable")
	d.Add(token.Position{Filename: "a.go", Line: 9}, "group-prefix", "function call result")
	d.Add(token.Position{Filename: "a.go", Line: 2}, "route", "non-literal expression")
	got := d.List()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Sorted by filename then line.
	wantOrder := []struct{ file string; line int }{
		{"a.go", 2}, {"a.go", 9}, {"b.go", 3},
	}
	for i, w := range wantOrder {
		if got[i].Pos.Filename != w.file || got[i].Pos.Line != w.line {
			t.Errorf("List()[%d] = %s:%d, want %s:%d", i, got[i].Pos.Filename, got[i].Pos.Line, w.file, w.line)
		}
	}
}

func TestDescribeExpr(t *testing.T) {
	cases := map[string]string{
		"package p; func f(xs []string){ _ = xs[0] }": "slice/map element",
		"package p; func f(){ _ = g() }":              "function call result",
		"package p; var _ = otherpkg.Path":            "identifier from another package",
	}
	for src, want := range cases {
		f := parseFile(t, src)
		var target ast.Expr
		ast.Inspect(f, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.IndexExpr:
				target = e
			case *ast.CallExpr:
				if _, isSel := e.Fun.(*ast.SelectorExpr); !isSel {
					target = e
				}
			case *ast.SelectorExpr:
				if target == nil {
					target = e
				}
			}
			return true
		})
		if got := DescribeExpr(target); got != want {
			t.Errorf("DescribeExpr(%q) = %q, want %q", src, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapter/astutil/ -run 'TestDiagnostics|TestDescribeExpr' -v`
Expected: FAIL — undefined `Diagnostics`, `DescribeExpr`.

- [ ] **Step 3: Implement diagnostics + DescribeExpr**

Append to `internal/adapter/astutil/resolve.go`:

Add `"sort"` to the `resolve.go` import block, then append:

```go
// Diagnostic records a route site the scanner could not statically resolve.
type Diagnostic struct {
	Pos    token.Position
	Kind   string // "route" | "group-prefix"
	Reason string
}

// Diagnostics is an append-only collector; List returns them in source order.
type Diagnostics struct {
	items []Diagnostic
}

func (d *Diagnostics) Add(pos token.Position, kind, reason string) {
	d.items = append(d.items, Diagnostic{Pos: pos, Kind: kind, Reason: reason})
}

func (d *Diagnostics) List() []Diagnostic {
	out := append([]Diagnostic(nil), d.items...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pos.Filename != out[j].Pos.Filename {
			return out[i].Pos.Filename < out[j].Pos.Filename
		}
		return out[i].Pos.Line < out[j].Pos.Line
	})
	return out
}

// DescribeExpr names why an expression is not a static string, for diagnostics.
func DescribeExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.IndexExpr:
		return "slice/map element"
	case *ast.CallExpr:
		return "function call result"
	case *ast.SelectorExpr:
		if _, ok := e.X.(*ast.Ident); ok {
			return "identifier from another package"
		}
		return "field or method value"
	case *ast.Ident:
		return "variable value" // a local, or a name not in the const table
	}
	return "non-literal expression"
}
```

Then add to `internal/adapter/astutil/astutil.go`, near the `Locator` methods (~line 146):

```go
// Position resolves a token.Pos to a file:line:col for diagnostics.
func (l Locator) Position(pos token.Pos) token.Position {
	if l.Fset == nil || !pos.IsValid() {
		return token.Position{}
	}
	return l.Fset.Position(pos)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/adapter/astutil/ -v` then `go vet ./internal/adapter/astutil/`
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/astutil/resolve.go internal/adapter/astutil/resolve_test.go internal/adapter/astutil/astutil.go
git commit -m "feat(astutil): diagnostics collector and Locator.Position"
```

---

### Task 3: Migrate the Adapter interface to return diagnostics; swap all route sites to ResolveString

This task must be atomic: the `Adapter.Scan` signature change and all eight adapters plus both call sites compile together. Diagnostics are wired but recorded as empty here; Task 4 populates them.

**Files:**
- Modify: all eight `internal/adapter/*/*.go` (see site map above)
- Modify: `spector.go:248`, `spector.go:530`
- Test: existing adapter tests must still pass; add const-route testdata for gin, chi, echo, stdlib.

**Interfaces:**
- Consumes: `astutil.StringConsts`, `astutil.ResolveString` (Task 1), `astutil.Diagnostics` (Task 2).
- Produces: `Adapter.Scan(dir string) ([]core.Route, map[string]*core.Schema, []astutil.Diagnostic, error)` for all eight adapters.

- [ ] **Step 1: Write the failing test (gin const route)**

Create `internal/adapter/gin/testdata/constroute/main.go`:

```go
package main

import "github.com/gin-gonic/gin"

const userPath = "/users/:id"

func main() {
	r := gin.Default()
	base := "/api/v1"
	r.GET(userPath, getUser)
	r.GET(base+"/health", health)
}

func getUser(c *gin.Context) {}
func health(c *gin.Context)  {}
```

Add to `internal/adapter/gin/gin_test.go` (match the existing test's scan-helper style — grep the file for how other testdata dirs are scanned and asserted; reuse that helper):

```go
func TestGinResolvesConstAndConcatRoutes(t *testing.T) {
	routes := scanRoutes(t, "testdata/constroute") // use the file's existing scan helper
	got := map[string]bool{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{"GET /users/:id", "GET /api/v1/health"} {
		if !got[want] {
			t.Errorf("missing route %q; got %v", want, got)
		}
	}
}
```

If the existing tests call `(&Adapter{}).Scan(dir)` directly, note it now returns four values; update the helper accordingly.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapter/gin/ -run TestGinResolvesConstAndConcatRoutes -v`
Expected: FAIL — routes missing (const/concat not resolved yet), or a compile error if the helper doesn't yet handle four returns.

- [ ] **Step 3: Change the Adapter interface and gin**

Find the `Adapter` interface declaration (grep `Scan(dir string) (\[\]core.Route`) and change it to:

```go
Scan(dir string) ([]core.Route, map[string]*core.Schema, []astutil.Diagnostic, error)
```

In `internal/adapter/gin/gin.go` `Scan`:
- After the files are parsed and `loc` is built, add: `consts := astutil.StringConsts(files)` and `var diags astutil.Diagnostics` (where `files` is the slice of `*ast.File` already available — grep for the `ast.Inspect(file` loop to find the file slice; if the adapter iterates `pkgs`, collect the files into a slice first).
- Thread `consts` and `&diags` into `collectGroups`/`collectRoutes` (or the inline walk) the same way `loc` is threaded.
- At line 107: `path, ok := astutil.ResolveString(call.Args[0], consts)`; on `!ok` add `diags.Add(loc.Position(call.Args[0].Pos()), "route", astutil.DescribeExpr(call.Args[0]))` (`DescribeExpr` is already exported from Task 2).
- At line 169 (group prefix): `prefix, ok := astutil.ResolveString(call.Args[0], consts)`; on `!ok`, `diags.Add(loc.Position(call.Args[0].Pos()), "group-prefix", astutil.DescribeExpr(call.Args[0]))`.
- Change the final `return routes, scanner.Schemas, nil` to `return routes, scanner.Schemas, diags.List(), nil`.

Update `spector.go:248` and `spector.go:530`:
- Line 248 `routes, _, err := adapterFor(cfg).Scan(cfg.Dir)` → `routes, _, _, err := adapterFor(cfg).Scan(cfg.Dir)`.
- Line 530 `routes, schemas, err := adapterFor(cfg).Scan(cfg.Dir)` → `routes, schemas, diags, err := adapterFor(cfg).Scan(cfg.Dir)` and stash `diags` where the result is assembled (add a field to whatever struct line 530 populates, or a local passed onward; grep the function to see the return path — the diagnostics must reach the library result for Task 5). If unclear, add `_ = diags` here and record a TODO-free note in the report that Task 5 wires it; but prefer wiring a `Diagnostics []astutil.Diagnostic` field on the result now.

- [ ] **Step 4: Convert the other seven adapters**

Apply the identical pattern to each adapter using the site map table. For each: build `consts` + `var diags`, thread them, swap each listed route-path/group-prefix `StringLit` to `ResolveString(arg, consts)` with a `diags.Add(loc.Position(arg.Pos()), kind, astutil.DescribeExpr(arg))` on the `!ok` branch, and return `diags.List()` as the new third value. Exact sites:
- chi: 118 (route), 150 (group-prefix)
- echo: 88, 98, 110 (route), 203 (group-prefix) — note 98 is `call.Args[1]`
- fiber: 89, 124 (route arg0), 111 (route arg1), 212 (group-prefix)
- gorillamux: 89 (route), 214 (group-prefix)
- httprouter: 107 (route arg0), 114 (route arg1); no group sites
- bunrouter: 104 (route), 138 (group-prefix)
- stdlib: 83 (route), 194 (group-prefix)

Leave verb/name sites literal-only: echo:160, fiber:100, gorillamux:162, httprouter:113.

For echo/fiber where the route site is inside an `if path, ok := astutil.StringLit(...); ok {` one-liner, expand it so the `!ok` branch can record a diagnostic:

```go
if path, ok := astutil.ResolveString(call.Args[0], consts); ok {
	// ... existing addRoute ...
} else {
	diags.Add(loc.Position(call.Args[0].Pos()), "route", astutil.DescribeExpr(call.Args[0]))
}
```

- [ ] **Step 5: Run the test to verify it passes and nothing regressed**

Run: `go build ./... && go test ./internal/adapter/... -v`
Expected: PASS — gin const/concat test passes; all existing adapter tests still pass (routes byte-identical for literal paths).

- [ ] **Step 6: Add const/concat testdata + tests for chi, echo, stdlib**

Mirror the gin fixture for chi, echo, and stdlib (create `testdata/constroute` with a const path and a `base + "/x"` concat route in each adapter's idiom), plus a matching `TestXxxResolvesConstAndConcatRoutes` asserting both routes appear. Use each adapter's existing test helper.

Run: `go test ./internal/adapter/chi/ ./internal/adapter/echo/ ./internal/adapter/stdlib/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/adapter spector.go
git commit -m "feat(adapter): resolve const/var/concat route paths across all routers"
```

---

### Task 4: Populate and assert dynamic-route diagnostics

Task 3 wired the collector but the diagnostic *content* is only exercised by adapter-internal `Add` calls. This task adds fixtures with genuinely dynamic routes and asserts the diagnostics.

**Files:**
- Add dynamic-route testdata to gin, chi, echo, stdlib `testdata/`
- Test: each adapter's `*_test.go`

**Interfaces:**
- Consumes: the four-return `Scan` from Task 3; `astutil.Diagnostic`.

- [ ] **Step 1: Write the failing test (gin dynamic route diagnostic)**

Create `internal/adapter/gin/testdata/dynroute/main.go`:

```go
package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	paths := []string{"/a", "/b"}
	for _, p := range paths {
		r.GET(p, h) // dynamic: path is a range variable
	}
}

func h(c *gin.Context) {}
```

Add to `internal/adapter/gin/gin_test.go`:

```go
func TestGinReportsDynamicRoute(t *testing.T) {
	_, _, diags, err := (&Adapter{}).Scan("testdata/dynroute")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 {
		t.Fatalf("diags = %d, want 1: %+v", len(diags), diags)
	}
	if diags[0].Kind != "route" {
		t.Errorf("kind = %q, want route", diags[0].Kind)
	}
	if diags[0].Pos.Line == 0 || diags[0].Pos.Filename == "" {
		t.Errorf("diagnostic has no source position: %+v", diags[0])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapter/gin/ -run TestGinReportsDynamicRoute -v`
Expected: FAIL if the fixture doesn't exist yet or the diagnostic isn't recorded. (If Task 3's gin conversion already records it, this test may pass immediately — that is acceptable; it locks the behaviour.)

- [ ] **Step 3: Confirm the diagnostic is recorded**

No new production code should be needed — Task 3 already calls `diags.Add(...)` on the `!ok` branch of the gin route site. If the test fails because no diagnostic is produced, verify the gin route site's `else` branch records it and that the range-variable `p` does not accidentally resolve (it must not appear in `consts`, since it is a local, not a package-level decl).

Run: `go test ./internal/adapter/gin/ -run TestGinReportsDynamicRoute -v`
Expected: PASS.

- [ ] **Step 4: Add dynamic-route fixtures + tests for chi, echo, stdlib**

Mirror the gin dynamic fixture and `TestXxxReportsDynamicRoute` for chi, echo, and stdlib.

Run: `go test ./internal/adapter/chi/ ./internal/adapter/echo/ ./internal/adapter/stdlib/ -run ReportsDynamicRoute -v`
Expected: PASS (each yields exactly one `route` diagnostic with a valid position).

- [ ] **Step 5: Commit**

```bash
git add internal/adapter
git commit -m "test(adapter): assert dynamic routes produce diagnostics"
```

---

### Task 5: Surface diagnostics on the library result and CLI stderr + -strict-routes

**Files:**
- Modify: `spector.go` (expose diagnostics on the result of the function at line 530)
- Modify: `cmd/spector/main.go`
- Test: `cmd/spector/main_test.go` (or the nearest CLI test file)

**Interfaces:**
- Consumes: the `diags` collected at `spector.go:530` (Task 3).
- Produces: a `-strict-routes` flag; stderr diagnostic lines.

- [ ] **Step 1: Write the failing CLI test**

Locate the CLI test harness (grep `func TestMain\|func run(\|os.Args` under `cmd/spector/`). Add a fixture-driven test that runs the generator against a source dir containing one dynamic route and captures stderr. Model it on the existing CLI tests' invocation style. The assertion:

```go
func TestCLIWarnsOnDynamicRoute(t *testing.T) {
	// Arrange: a temp source dir with one gin dynamic route (reuse the
	// gin testdata/dynroute layout via a small copy helper, or point -dir at it).
	var stderr bytes.Buffer
	code := runMain(t, &stderr, "-dir", "../../internal/adapter/gin/testdata/dynroute")
	if code != 0 {
		t.Errorf("default run should exit 0, got %d", code)
	}
	if !strings.Contains(stderr.String(), "dynamic route") {
		t.Errorf("stderr missing dynamic-route warning:\n%s", stderr.String())
	}

	stderr.Reset()
	code = runMain(t, &stderr, "-strict-routes", "-dir", "../../internal/adapter/gin/testdata/dynroute")
	if code == 0 {
		t.Errorf("-strict-routes should exit non-zero when a route is dynamic")
	}
}
```

Adapt `runMain` to however the CLI is invoked in existing tests (it may be `main()` with `os.Args`, or an extracted `run(args, stdout, stderr) int`). If the CLI has no extracted `run`, extract one in Step 3 so the test can capture stderr and the exit code.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/spector/ -run TestCLIWarnsOnDynamicRoute -v`
Expected: FAIL — no diagnostic output, no `-strict-routes` flag.

- [ ] **Step 3: Wire diagnostics through and print them**

In `spector.go`, ensure the diagnostics collected at line 530 reach the CLI. The cleanest path: have the function return them (or set them on its result struct). Grep the function containing line 530 for its signature and return type; add a `[]astutil.Diagnostic` alongside the existing result (e.g. a `RouteDiagnostics` field, or an extra return value consumed by `cmd/spector`).

In `cmd/spector/main.go`:
- Add the flag: `strictRoutes := flag.Bool("strict-routes", false, "exit non-zero if any route path cannot be statically resolved")`.
- After generation, for each diagnostic print to stderr:
  `fmt.Fprintf(os.Stderr, "spector: %s: dynamic %s, cannot infer path (%s)\n", d.Pos, d.Kind, d.Reason)` and, if any exist, a summary `fmt.Fprintf(os.Stderr, "spector: %d route(s) could not be statically resolved\n", n)`.
- If `*strictRoutes && n > 0`, return a non-zero exit code.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/spector/ -run TestCLIWarnsOnDynamicRoute -v`
Expected: PASS (default exit 0 with a warning; `-strict-routes` exits non-zero).

- [ ] **Step 5: Commit**

```bash
git add spector.go cmd/spector
git commit -m "feat(cli): warn on dynamic routes; add -strict-routes"
```

---

### Task 6: Full suite and README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Run the full suite**

Run: `go test ./... -timeout 180s`
Expected: PASS across all packages. If a golden test changed unexpectedly (a literal route whose output shifted), stop and investigate — literal routes must be byte-identical.

- [ ] **Step 2: Document the feature**

Find the routing/limitations section in `README.md` (`grep -n -i "route\|literal\|limitation" README.md`). Add a short paragraph, matching the surrounding voice: Spector now resolves route paths and group prefixes built from package-level string constants/vars and `+` concatenations, not just string literals; routes whose path is genuinely dynamic (built in a loop, from a slice/map, or a function return) are reported to stderr as diagnostics, and `-strict-routes` turns those into a non-zero exit. Remove or update any existing limitation bullet that claims only literal routes are detected.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document static route resolution and -strict-routes"
```

---

## Self-Review

**Spec coverage:**
- Resolve const/var/ident/concat → Task 1 (`StringConsts`/`ResolveString`). ✓
- Diagnostics with file:line + reason → Tasks 2 (collector/describe) + 3–4 (recording). ✓
- All eight adapters → Task 3 (site map covers every route/prefix site). ✓
- Diagnostic for unresolved sites → Task 4. ✓
- Stderr surfacing + `-strict-routes`, default exit 0 → Task 5. ✓
- Library exposes diagnostics → Task 3/5 (result field). ✓
- Literal routes byte-identical, drop-in `ResolveString(_, nil)` → Task 1 test `TestResolveStringLiteralDropIn`, guarded by Task 6 full-suite golden check. ✓
- No new deps, no annotations, stderr-not-stdout → Global Constraints, honoured in Task 5. ✓

**Placeholder scan:** One soft spot — Task 3 Step 3 / Task 5 Step 3 both depend on the exact shape of the function at `spector.go:530`, which the implementer must grep. This is unavoidable without pasting that function here; the instruction is concrete (add a `[]astutil.Diagnostic` field/return and thread it) and names the exact line. No "TBD"/"handle edge cases" placeholders remain.

**Type consistency:** `ResolveString(ast.Expr, map[string]string) (string, bool)`, `StringConsts([]*ast.File) map[string]string`, `Diagnostic{Pos token.Position; Kind, Reason string}`, `Diagnostics.Add/List`, `Locator.Position(token.Pos) token.Position`, and the new `Scan(...) ([]core.Route, map[string]*core.Schema, []astutil.Diagnostic, error)` are used identically across Tasks 1–5. `DescribeExpr` is exported from Task 2 onward, so the adapters in sibling packages (Task 3) call it without a mid-plan rename.
