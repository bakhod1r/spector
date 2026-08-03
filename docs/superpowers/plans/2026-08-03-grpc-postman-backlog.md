# gRPC + Postman Backlog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the 7 deliberately-deferred limitations in `docs/grpc-plan.md` and `docs/postman-plan.md` (gRPC streaming/TLS/schema/imports, Postman JSONPath/import/pre-request script).

**Architecture:** Each item is independent, gets its own task, test cycle, and commit. gRPC changes touch `internal/grpcx/invoke.go` + `internal/proto/proto.go` + the gRPC section of `internal/ui/ui.html`. Postman changes are all in `internal/ui/ui.html` (single self-contained file, state in localStorage).

**Tech Stack:** Go (grpcurl, emicklei/proto, protoreflect), vanilla JS single-file UI, Go `testing`, Playwright (`e2e/console.spec.js`).

## Global Constraints

- UI is one self-contained file `internal/ui/ui.html`; NO external CDN/assets. All new JS lives inside it.
- Server surface stays minimal; only `internal/grpcx.Request` gains fields (G1/G2).
- Follow existing patterns; add tests to existing test files, don't restructure.
- Module path: `github.com/user/specter`.
- Run Go tests from repo root: `go test ./internal/...`. Run e2e from repo root per existing `e2e` setup.
- Deliberately OUT of scope: mutual-TLS/CA files (G2), full JSONPath grammar unions/scripts (P1), proto2/custom options (G4), full JS isolation via Worker/iframe (P3), Postman formdata/file bodies & separate environment import (P2).

---

## Task 1 (G1): client-stream / bidi Execute

**Files:**
- Modify: `internal/grpcx/invoke.go:51-55` (parser already streams; verify multi-message)
- Modify: `internal/ui/ui.html` (gRPC card `invokeGrpc()` ~line 1543, payload ~1552)
- Test: `internal/grpcx/invoke_test.go`

**Interfaces:**
- Consumes: existing `Invoke(protoDir string, req Request) (string, error)`, `Request{Target,Symbol,Data,Headers}`.
- Produces: no signature change. `req.Data` MAY contain multiple whitespace/newline-separated JSON objects; `grpcurl.RequestParserAndFormatter` + `rf.Next` already emit one per object into a client/bidi stream.

- [ ] **Step 1: Write the failing test**

Add to `internal/grpcx/invoke_test.go`. Use the existing test gRPC server pattern from `live_test.go` (a client-streaming method that counts messages). If no client-streaming method exists on the test server, add one that returns the count.

```go
func TestInvokeClientStreamSendsAllMessages(t *testing.T) {
	target := startTestServer(t) // reuse existing helper from live_test.go
	req := Request{
		Target: target,
		Symbol: "shop.v1.UserService/CountUsers", // client-streaming rpc returning {count}
		Data:   "{\"id\":1}\n{\"id\":2}\n{\"id\":3}",
	}
	out, err := Invoke("", req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !strings.Contains(out, "\"count\": 3") && !strings.Contains(out, "\"count\":3") {
		t.Fatalf("expected count 3, got: %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/grpcx/ -run TestInvokeClientStreamSendsAllMessages -v`
Expected: FAIL (test server lacks client-streaming method, or count != 3).

- [ ] **Step 3: Add the client-streaming method to the test server**

In the test server type in `internal/grpcx/live_test.go`, implement a client-streaming `CountUsers` that reads all messages and returns the count. (Backend `Invoke` needs no change — `rf.Next` already feeds each parsed object.) Register it. If the proto/test stub does not define it, add a minimal streaming method to the existing test service stub used by these tests.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/grpcx/ -run TestInvokeClientStreamSendsAllMessages -v`
Expected: PASS. Confirms multi-object `Data` reaches the stream.

- [ ] **Step 5: UI — multi-message input for client/bidi**

In `internal/ui/ui.html` gRPC method card: when the method badge is `client` or `bidi`, render an "＋ Add message" button that appends extra `<textarea>` inputs. In `invokeGrpc()` (~1543), join all message textareas with `"\n"` into the `data` field before POST. Unary/server-stream keep the single textarea.

```js
// inside invokeGrpc(): collect all message bodies for streaming methods
const msgs = card.querySelectorAll(".grpc-msg");
const data = Array.from(msgs).map(t => interpolate(t.value, activeEnv())).join("\n");
// ...existing POST with { target, symbol, data }
```

- [ ] **Step 6: Commit**

```bash
git add internal/grpcx/ internal/ui/ui.html
git commit -m "feat(grpc): send multiple messages for client-stream/bidi Execute"
```

---

## Task 2 (G2): TLS + configurable timeout

**Files:**
- Modify: `internal/grpcx/invoke.go:19-24` (Request), `:30` (timeout), `:33` (creds)
- Modify: `internal/ui/ui.html` (gRPC TLS toggle + grpcurl command builder)
- Test: `internal/grpcx/invoke_test.go`

**Interfaces:**
- Produces: `Request` gains `TLS bool`, `Insecure bool`, `TimeoutSec int` (JSON: `tls`, `insecure`, `timeoutSec`). New helper `dialCreds(req Request) credentials.TransportCredentials`.

- [ ] **Step 1: Write the failing test**

```go
func TestDialCredsSelectsTLS(t *testing.T) {
	if got := dialCreds(Request{}); got.Info().SecurityProtocol != "insecure" {
		t.Fatalf("default should be insecure, got %q", got.Info().SecurityProtocol)
	}
	if got := dialCreds(Request{TLS: true}); got.Info().SecurityProtocol == "insecure" {
		t.Fatalf("TLS:true should not be insecure")
	}
}

func TestTimeoutDefault(t *testing.T) {
	if d := timeoutOf(Request{}); d != 15*time.Second {
		t.Fatalf("default timeout = %v", d)
	}
	if d := timeoutOf(Request{TimeoutSec: 5}); d != 5*time.Second {
		t.Fatalf("custom timeout = %v", d)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/grpcx/ -run 'TestDialCreds|TestTimeout' -v`
Expected: FAIL ("undefined: dialCreds", "undefined: timeoutOf").

- [ ] **Step 3: Implement fields + helpers**

In `internal/grpcx/invoke.go` add to `Request`:

```go
	TLS        bool `json:"tls,omitempty"`
	Insecure   bool `json:"insecure,omitempty"`   // TLS but skip cert verify
	TimeoutSec int  `json:"timeoutSec,omitempty"` // 0 -> 15s
```

Add helpers and wire them in `Invoke`:

```go
func dialCreds(req Request) credentials.TransportCredentials {
	if req.TLS {
		return credentials.NewTLS(&tls.Config{InsecureSkipVerify: req.Insecure})
	}
	return insecure.NewCredentials()
}

func timeoutOf(req Request) time.Duration {
	if req.TimeoutSec > 0 {
		return time.Duration(req.TimeoutSec) * time.Second
	}
	return 15 * time.Second
}
```

Replace `:30` `context.WithTimeout(..., 15*time.Second)` with `timeoutOf(req)` and `:33` `grpc.WithTransportCredentials(insecure.NewCredentials())` with `grpc.WithTransportCredentials(dialCreds(req))`. Add imports `crypto/tls` and `google.golang.org/grpc/credentials`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/grpcx/ -run 'TestDialCreds|TestTimeout' -v`
Expected: PASS.

- [ ] **Step 5: UI — TLS toggle + grpcurl flags**

In `internal/ui/ui.html`: add a TLS checkbox (and an "Insecure (skip verify)" checkbox) to the gRPC panel, backed by env vars `grpcTLS`/`grpcInsecure`. Include `tls`/`insecure`/`timeoutSec` in the `invokeGrpc()` POST body. In the "Copy as grpcurl" builder, emit `-plaintext` only when TLS is off, and `-insecure` when insecure is on.

```js
const tls = activeEnv().vars.grpcTLS === "true";
const insecure = activeEnv().vars.grpcInsecure === "true";
// grpcurl: (tls ? (insecure ? "-insecure " : "") : "-plaintext ")
```

- [ ] **Step 6: Commit**

```bash
git add internal/grpcx/ internal/ui/ui.html
git commit -m "feat(grpc): TLS transport + configurable timeout"
```

---

## Task 3 (G3): oneof / Any / well-known types

**Files:**
- Modify: `internal/proto/proto.go:98-112` (messageToSchema), `:134-150` (scalarSchema)
- Test: `internal/proto/scalar_test.go`, `internal/proto/proto_test.go`

**Interfaces:**
- Produces: `scalarSchema` recognizes well-known type names. `messageToSchema` handles `*proto.Oneof`, adding a `x-oneof` marker. Requires `core.Schema` to carry the marker — add field `XOneof [][]string \`json:"x-oneof,omitempty\"\`` to `core.Schema` if absent.

- [ ] **Step 1: Write the failing tests**

Add to `internal/proto/scalar_test.go`:

```go
func TestWellKnownScalars(t *testing.T) {
	cases := map[string]struct{ typ, format string }{
		"google.protobuf.Timestamp": {"string", "date-time"},
		"google.protobuf.Duration":  {"string", "duration"},
		"google.protobuf.Empty":     {"object", ""},
	}
	for in, want := range cases {
		s := scalarSchema(in)
		if s.Type != want.typ || s.Format != want.format {
			t.Errorf("%s -> {%s,%s}, want {%s,%s}", in, s.Type, s.Format, want.typ, want.format)
		}
	}
	if a := scalarSchema("google.protobuf.Any"); a.Properties["@type"] == nil {
		t.Errorf("Any should expose @type property")
	}
}
```

Add to `internal/proto/proto_test.go` a message with a `oneof` (parse from a small proto string via `proto.NewParser`) and assert the resulting schema has both variant properties AND a non-empty `XOneof` group listing them.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/proto/ -run 'TestWellKnown|TestOneof' -v`
Expected: FAIL (well-known types return message refs; oneof variants absent; `XOneof` undefined).

- [ ] **Step 3: Implement well-known + oneof**

If `core.Schema` lacks it, add `XOneof [][]string` field. In `scalarSchema` add cases BEFORE the `default`:

```go
	case "google.protobuf.Timestamp":
		return &core.Schema{Type: "string", Format: "date-time"}
	case "google.protobuf.Duration":
		return &core.Schema{Type: "string", Format: "duration"}
	case "google.protobuf.Struct", "google.protobuf.Value":
		return &core.Schema{Type: "object"}
	case "google.protobuf.Empty":
		return &core.Schema{Type: "object"}
	case "google.protobuf.Any":
		return &core.Schema{Type: "object", Properties: map[string]*core.Schema{"@type": {Type: "string"}}}
	case "google.protobuf.StringValue":
		return &core.Schema{Type: "string"}
	case "google.protobuf.Int32Value", "google.protobuf.Int64Value":
		return &core.Schema{Type: "integer"}
	case "google.protobuf.BoolValue":
		return &core.Schema{Type: "boolean"}
	case "google.protobuf.DoubleValue", "google.protobuf.FloatValue":
		return &core.Schema{Type: "number"}
```

In `messageToSchema`, add a case for oneof and collect its variant names:

```go
		case *proto.Oneof:
			group := []string{}
			for _, oe := range f.Elements {
				if of, ok := oe.(*proto.OneOfField); ok {
					schema.Properties[of.Name] = fieldSchema(of.Type, false)
					group = append(group, of.Name)
				}
			}
			if len(group) > 0 {
				schema.XOneof = append(schema.XOneof, group)
			}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/proto/ -run 'TestWellKnown|TestOneof' -v`
Expected: PASS.

- [ ] **Step 5: Full package test**

Run: `go test ./internal/proto/ ./internal/...`
Expected: PASS (no regression in existing scalar/proto tests).

- [ ] **Step 6: Commit**

```bash
git add internal/proto/ internal/core/
git commit -m "feat(proto): well-known types + oneof schema marker"
```

---

## Task 4 (G4): FileDescriptor import-resolve

**Files:**
- Modify: `internal/proto/proto.go:12-60` (Scan — key by package-qualified name, follow imports)
- Test: `internal/proto/proto_test.go`, testdata under `internal/proto/testdata/`

**Interfaces:**
- Produces: `Scan` keys the message map by fully-qualified name (`pkg.Message`); `$ref` values point to the qualified name; cross-file references resolve. `collect`/`walk` operate on qualified names.

- [ ] **Step 1: Add testdata + failing test**

Create `internal/proto/testdata/common.proto`:

```proto
syntax = "proto3";
package shop.v1;
message Money { int64 units = 1; int32 nanos = 2; }
```

Create `internal/proto/testdata/orders.proto`:

```proto
syntax = "proto3";
package shop.v1;
import "common.proto";
message Order { int32 id = 1; Money total = 2; }
service OrderService { rpc GetOrder(Order) returns (Order); }
```

Add test:

```go
func TestScanResolvesCrossFileImport(t *testing.T) {
	doc, err := Scan("testdata")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	order := doc.Messages["shop.v1.Order"]
	if order == nil {
		t.Fatal("Order message missing (expected qualified key)")
	}
	total := order.Properties["total"]
	if total == nil || total.Ref != "#/components/schemas/shop.v1.Money" {
		t.Fatalf("total ref = %+v, want ref to shop.v1.Money", total)
	}
	if doc.Messages["shop.v1.Money"] == nil {
		t.Fatal("Money (imported) not collected")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proto/ -run TestScanResolvesCrossFileImport -v`
Expected: FAIL (messages keyed by bare name `Order`/`Money`, ref is `#/.../Money` not qualified).

- [ ] **Step 3: Key by qualified name, resolve refs**

In `Scan`, capture each file's package, and store messages/enums as `pkg + "." + name`. In `scalarSchema`'s `default` ref branch, the message name may be bare in the proto source — resolve it against the current file's package: build the ref as `#/components/schemas/<pkg>.<type>` when `<type>` is unqualified. Thread the current package into `messageToSchema`/`fieldSchema`/`scalarSchema` (add a `pkg string` param), or resolve refs in a post-pass that qualifies any bare ref against the message's own package. Update `serviceToGrpc` so `InputType`/`OutputType` are qualified too. `collect`/`walk` already follow `$ref`; they now traverse qualified keys.

Imports: emicklei `proto.WithImport` can be walked, but since all testdata files are scanned anyway (`protoFiles` walks the dir), the key change is consistent qualified naming so a ref from `orders.proto` finds `Money` from `common.proto`. Keep it simple: qualify by package, no separate import graph needed when all files share the scan dir.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/proto/ -run TestScanResolvesCrossFileImport -v`
Expected: PASS.

- [ ] **Step 5: Update existing proto tests for qualified keys**

Existing `proto_test.go` assertions using bare names (e.g. `doc.Messages["User"]`) must become `doc.Messages["shop.v1.User"]`. Run full package and fix each:

Run: `go test ./internal/proto/ -v`
Expected: PASS after updating key expectations.

- [ ] **Step 6: Commit**

```bash
git add internal/proto/
git commit -m "feat(proto): qualify message names by package for cross-file refs"
```

---

## Task 5 (P1): Full-ish JSONPath in `pick()`

**Files:**
- Modify: `internal/ui/ui.html:881-887` (`pick`), callers `:891-892`, `:920`
- Test: `e2e/console.spec.js`

**Interfaces:**
- Produces: `pick(obj, path)` returns an array of matches for wildcard/filter/recursive paths, and the single value for a plain path. Callers adapt: extract uses first match; `jsonExists` = at least one match; `jsonEquals` compares first match.

- [ ] **Step 1: Write the failing e2e assertions**

In `e2e/console.spec.js`, add a test that loads a response fixture and evaluates `pick` in the page context (via `page.evaluate`) for:

```js
// $.users[*].name  -> ["ada","bob"]
// $..id            -> [1,2,3]
// $.users[?(@.role=='admin')].name -> ["ada"]
```

Assert each returns the expected matches.

- [ ] **Step 2: Run to verify it fails**

Run the e2e suite for this spec (per existing `e2e` run command).
Expected: FAIL (current `pick` returns undefined for `[*]`, `..`, `[?()]`).

- [ ] **Step 3: Rewrite `pick` as a small evaluator**

Replace `pick` with a tokenizing evaluator supporting `.key`, `[n]`, `[-1]`, `[*]`, `.*`, `..key`, and `[?(@.k=='v')]` / `[?(@.k!='v')]` / `[?(@.k)]`. Return an array of matches; add a `pickOne(obj,path)` that returns the first match (or undefined) for callers that need a scalar.

```js
function pick(obj, path) {
  if (!path || path === "$") return [obj];
  const toks = tokenizeJsonPath(path); // e.g. [{key},{index},{wild},{recurse,key},{filter,k,op,v}]
  let nodes = [obj];
  for (const t of toks) nodes = nodes.flatMap(n => stepJsonPath(n, t)).filter(v => v !== undefined);
  return nodes;
}
function pickOne(obj, path) { const r = pick(obj, path); return r.length ? r[0] : undefined; }
```

Implement `tokenizeJsonPath` and `stepJsonPath` (wildcard expands object/array; recurse descends all nodes matching key; filter keeps array items where `@.k op v` holds).

- [ ] **Step 4: Update callers**

- `:920` extract: `let val = pickOne(json, rule.jsonPath);`
- `:891` jsonExists: `const v = pick(bodyJson, t.target); ok = v.length > 0;`
- `:892` jsonEquals: `const v = pickOne(bodyJson, t.target); ok = JSON.stringify(v) === JSON.stringify(coerce(t.expected));`

- [ ] **Step 5: Run to verify it passes**

Run the e2e suite for this spec.
Expected: PASS. Also manually confirm a plain `$.id` still works (regression).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/ui.html e2e/console.spec.js
git commit -m "feat(ui): JSONPath wildcard, recursive descent, and filters in pick()"
```

---

## Task 6 (P2): Postman v2.1 collection import

**Files:**
- Modify: `internal/ui/ui.html:319-359` (parseImport/mergeStore), `:371-382` (importStore)
- Test: `e2e/console.spec.js`

**Interfaces:**
- Produces: `isPostmanV21(json)` detector; `postmanToStore(json)` mapper returning `{collections:[...]}` in Specter's shape. `importStore` routes to it when detected, else the existing `parseImport`.

- [ ] **Step 1: Write the failing e2e test**

In `e2e/console.spec.js`, import a small Postman v2.1 collection JSON:

```json
{ "info": { "name": "demo", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json" },
  "item": [ { "name": "Get user", "request": {
    "method": "GET", "header": [{"key":"X-Trace","value":"{{traceId}}"}],
    "url": { "raw": "{{baseUrl}}/users/1", "path": ["users","1"], "query": [{"key":"q","value":"ada"}] },
    "auth": { "type": "bearer", "bearer": [{"key":"token","value":"{{token}}"}] } } } ] }
```

Assert after import a collection "demo" exists with one request: method GET, path containing `users`, query `q=ada`, header `X-Trace={{traceId}}`, auth type bearer.

- [ ] **Step 2: Run to verify it fails**

Run the e2e suite.
Expected: FAIL (`parseImport` rejects — no `specter.collection` format marker).

- [ ] **Step 3: Add detector + mapper**

```js
function isPostmanV21(j) {
  return j && j.info && Array.isArray(j.item) &&
         (!j.format) && (String(j.info.schema||"").includes("v2.1.0") || j.info._postman_id);
}
function postmanToStore(j) {
  const requests = [];
  (function walk(items){ for (const it of (items||[])) {
    if (it.item) { walk(it.item); continue; }         // folder
    const r = it.request || {};
    const url = r.url || {};
    requests.push({
      id: uid(), name: it.name || "request",
      method: (r.method||"GET").toLowerCase(),
      path: "/" + (Array.isArray(url.path) ? url.path.join("/") : ""),
      pathParams: {},
      queryParams: Object.fromEntries((url.query||[]).map(q=>[q.key,q.value||""])),
      headers: Object.fromEntries((r.header||[]).map(h=>[h.key,h.value||""])),
      body: r.body && r.body.mode === "raw" ? (r.body.raw||"") : "",
      auth: mapPostmanAuth(r.auth), extract: [], tests: [],
      notes: (it.event||[]).map(e=>`${e.listen}: ${(e.script&&e.script.exec||[]).join("\\n")}`).join("\\n\\n") || undefined,
    });
  }})(j.item);
  return { collections: [{ id: uid(), name: j.info.name || "Imported", requests }] };
}
function mapPostmanAuth(a) {
  if (!a) return { type: "none" };
  if (a.type === "bearer") return { type: "bearer", value: kv(a.bearer,"token") };
  if (a.type === "basic")  return { type: "basic", username: kv(a.basic,"username"), password: kv(a.basic,"password") };
  if (a.type === "apikey") return { type: "apiKey", key: kv(a.apikey,"key"), value: kv(a.apikey,"value"), in: kv(a.apikey,"in")||"header" };
  return { type: "none" };
}
function kv(arr, name) { const f = (arr||[]).find(x=>x.key===name); return f ? f.value : ""; }
```

Match the `SavedRequest`/`Auth` field names already used in `ui.html` (adjust `auth` shape to the existing one — e.g. if auth stores a `fields` map, write into that instead). Add `notes` to `SavedRequest` if it does not exist (used again in P3).

- [ ] **Step 4: Route in importStore**

In `importStore()` (~371), before calling `parseImport`, branch:

```js
if (isPostmanV21(parsed)) {
  const incoming = postmanToStore(parsed);
  // reuse existing replace/merge confirm dialog + mergeStore()
  mergeOrReplace(incoming);   // same path the native import uses
  return;
}
```

Reuse the existing replace/merge confirm dialog and `mergeStore`.

- [ ] **Step 5: Run to verify it passes**

Run the e2e suite.
Expected: PASS. Also confirm native `specter.collection` import still works (regression).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/ui.html e2e/console.spec.js
git commit -m "feat(ui): import Postman v2.1 collections"
```

---

## Task 7 (P3): Pre-request script (constrained sandbox)

**Files:**
- Modify: `internal/ui/ui.html` (`send()` ~898, request editor, `SavedRequest`)
- Test: `e2e/console.spec.js`

**Interfaces:**
- Produces: `SavedRequest.preRequestScript: string`; `runPreRequest(req, env)` executes it in a constrained `Function` with only a `pm` argument. Runs BEFORE `buildURL`/`buildHeaders` in `send()`. Imported scripts (P2 `notes`) are NOT auto-run.

**SECURITY:** `new Function` runs user-authored code in-origin. The `pm`-only argument hides globals from casual scripts but is NOT a true sandbox (adversarial code can reach globals via constructor chains). Acceptable because the user authors/pastes their own script into their own console. Imported (foreign) scripts must NOT auto-execute; require an explicit opt-in. Full isolation (Worker/iframe) is future work.

- [ ] **Step 1: Write the failing e2e test**

In `e2e/console.spec.js`, create a saved request with `preRequestScript = "pm.environment.set('token','abc')"`, then send it and assert the outgoing request used `token=abc` where `{{token}}` appears (e.g. in a header). Also assert `pm.window` is `undefined` inside the script (write a script `pm.environment.set('leak', String(typeof pm.window))` and expect `undefined`).

- [ ] **Step 2: Run to verify it fails**

Run the e2e suite.
Expected: FAIL (`preRequestScript` unsupported; nothing runs).

- [ ] **Step 3: Implement the pm API + runner**

```js
function pmApi(env) {
  return {
    environment: { get: n => env.vars[n], set: (n,v) => { env.vars[n] = String(v); } },
    variables:   { get: n => env.vars[n], set: (n,v) => { env.vars[n] = String(v); } },
  };
}
function runPreRequest(req, env) {
  if (!req.preRequestScript) return;
  const fn = new Function("pm", '"use strict";\n' + req.preRequestScript);
  fn(pmApi(env)); // only pm is in scope; no window/fetch/document passed
}
```

- [ ] **Step 4: Wire into send() + editor**

At the top of `send()` (before `buildURL` ~900): `try { runPreRequest(req, activeEnv()); } catch (e) { /* surface as response error */ }`. Add a "Pre-request Script" textarea to the request editor bound to `req.preRequestScript`. Do NOT run `req.notes` (imported scripts) automatically.

- [ ] **Step 5: Run to verify it passes**

Run the e2e suite.
Expected: PASS (token set to abc; `pm.window` undefined).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/ui.html e2e/console.spec.js
git commit -m "feat(ui): pre-request script with constrained pm sandbox"
```

---

## Self-Review notes

- **Spec coverage:** G1–G4, P1–P3 each map to Tasks 1–7. All 7 spec items covered.
- **Type consistency:** `Request` fields (`tls`,`insecure`,`timeoutSec`) used identically in G2 test/impl/UI. `pick()`→array + `pickOne()` used consistently across P1 callers and reused by P2? (P2 doesn't use pick). `core.Schema.XOneof` defined in G3, used only there. `SavedRequest.notes` introduced in P2, referenced (not executed) in P3.
- **Ambiguity fixed:** G4 keys everything by `pkg.Name`; existing bare-name test assertions must be updated (Task 4 Step 5).
- **Verify before impl:** Task implementers must confirm the exact `Auth` shape and `SavedRequest` fields in `ui.html` before P2/P3 (the plan flags this in Task 6 Step 3).
```
