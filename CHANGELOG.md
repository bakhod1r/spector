# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.1] - 2026-08-20

### Fixed
- Calls out of a handler are followed into the declaration they actually name.
  They were resolved by bare name against a tree-wide table, and `decode`,
  `Handle` and `Append` are each declared several times in any layered project:
  the first one parsed answered for all of them. A JWT `decode(claims)` on the
  auth path therefore stepped into an audit codec's `decode(payload []byte)`,
  whose `json.Unmarshal` was read as the endpoint's request body — so four HTTP
  operations documented a Kafka wire struct as their input. Resolution now uses
  the same package-aware rules a route's handler does.
- Struct declarations are indexed by the package they are in. `Handler`,
  `Envelope` and `Response` are each declared once per bounded context, and a
  name-keyed table let the first one answer for every context: a handler read
  its own state through another context's field list and found nothing. A
  context that declares its own envelope is now documented against that one,
  falling back to a shared helper's only when a single package declares it.
- A list built with `make([]T, 0, n)` names its payload. It is the ordinary way
  a handler that returns a collection builds one — more common than
  `[]T{}`, because the length is usually known — and the response documented as
  nothing at all.
- A handler that answers out of its receiver's own store — `u, ok := h.users[id]`
  — names its payload. The type is on the struct field, nowhere in the body.
- The console's endpoint filter works while a request panel is open. Saved and
  ad-hoc request cards reuse the `.op` class and carry no filter key; reading
  one threw on the first card and killed the whole filter, leaving every
  category showing its full count and every operation visible, as if nothing
  had been typed.
- The console's category sidebar follows the search. It is the index of the
  list beside it and was left untouched, so it contradicted the pane: full
  counts, and rows for operations the search had just hidden — clicking one
  scrolled to something no longer on screen.
- The console's search survives a tab switch. Moving between REST, gRPC,
  GraphQL and Realtime rebuilt the pane unfiltered while the search box kept
  its text, so the box named a filter the list beside it was not applying.
- A search for a category's name shows that category's operations in the pane,
  not only in the sidebar. The sidebar already matched on the group name; the
  pane matched only per-operation, so a tag taken from `tags` rather than from
  the path listed five operations in the index and none in the list.
- The console's Mock button works without a restart. The mock route existed
  only when the process was started with mocking on, so the switch a reader
  flips while reading usually did nothing.
- The lint job runs. `.golangci.yml` is a `version: "2"` config and the action
  installed the newest v1, which rejected it outright; the config had therefore
  never been enforced, and it caught a dead method (`FuncIndex.recvType`) on
  its first successful run.
- The browser job starts its broker. Installing the mosquitto package also
  starts one under systemd on port 1883, so the suite's own broker exited on
  "Address already in use" and its websocket listener on 9001 never opened —
  which the wait loop reported only as "broker never came up". The packaged
  service is stopped first, and a broker that still refuses to start is now
  run in the foreground so it says why.

## [0.5.0] - 2026-08-20

### Changed
- **The module is `github.com/bakhod1r/spector`.** The project was spelled
  `specter` through 0.4.0; the import path, the command, the binary and the
  config file (`spector.json`, `spector.yaml`) all follow the new spelling, as
  do the `spector:` doc directives. An existing install keeps working — the old
  tags are still fetchable under the old path — but an upgrade is a rewrite of
  the import path, not a version bump.
- The minimum Go version is 1.26.6, which closes 16 advisories the previous
  1.26.2 floor left reachable, among them an infinite loop in the HTTP/2
  transport (`GO-2026-4918`) that `-serve` and the proxy both route through.
  `govulncheck` now runs on every CI build rather than at release time.

### Added
- Responses wrapped in a project's own envelope helper are documented as the
  payload they carry. A service where every handler answers through
  `OK(c, data)` used to document eighty operations as the same `Envelope` with
  an `any` field, and a client generated from it had no types at all — a silent
  failure, because the document looked complete. The pairing of the envelope
  with the payload it was handed is now registered as its own schema, and the
  envelope's other fields (`meta`, `error`) survive.
- Routes are resolved through the layers a service grows once it passes one
  package: bounded contexts that each declare `Mount`/`Create`, registrations
  spread as `append(guards, h.X)...`, handler factories, and a group prefix
  that reaches the registration three hops away. Covered for all eight
  adapters.
- Prebuilt binaries. Tagging `v*` builds linux, macOS and Windows archives for
  amd64 and arm64, with checksums, and publishes them to the GitHub release.
  They carry the `mcp` and `grpclive` build tags: those tags exist to keep a
  source install small, and someone who downloaded a binary has no way to
  rebuild it with a tag they turn out to need.
- `spector -V` prints the version of the binary. `-version` was already taken —
  it is the version of the API being documented — so renaming it would have
  silently changed what every existing invocation writes into the document.

### Fixed
- The coverage floor measures the packages the project ships. The examples and
  their generated protobuf stubs counted as uncovered code, which held the
  reported total at 88.2% — below the 90% floor — for demo files nothing is
  meant to test. Scoped to the shipping packages it is 91.5%.

## [0.4.0] - 2026-08-20

### Fixed
- Test files are no longer scanned. A router built by an `admission_test.go`
  was documented as part of the API (`GET /`), collided with the real route it
  duplicated, and cost that operation its `operationId` — which the generated
  SDK then emitted as an unnamed, argument-less method.
- The scan reads the directories below `-dir`. Pointing it at a backend root
  reported "no routes found" and the caller had to name the exact package;
  the warning now also names the adapter in use and says the scan is recursive.
- A router group passed to a function instead of being assigned keeps its
  prefix: `registerMFARoutes(private.Group("/auth/mfa"), c)` documents
  `/api/v1/auth/mfa/...` rather than flattening every route inside to its bare
  path. Prefixes compose through nested groups and through a second hop, and
  parameter names are scoped per function, so the `r` in one function cannot
  rewrite the `r` in another.
- Handlers that reach the framework through package helpers are inspected
  through them, up to two calls deep, carrying the caller's argument types and
  status codes into the callee's parameters. A project with an `httpx`-style
  wrapper used to document paths and middleware and nothing else — no request
  body, no response type, no schemas. Cross-package handlers
  (`r.POST("/x", h.Handler{}.Create)`) are resolved for the same reason.
- A helper whose name matches a framework verb is stepped into like any other.
  `httpx.BindJSON(c, &req)` is spelled exactly like gin's own `c.BindJSON`, so
  the scan matched the name and stopped: the request type was lost, and so was
  every status the helper writes (the `400` inside a bind wrapper). Handlers
  that bind through a wrapper in another package now document their request
  body and its schema.
- A response passed as a call is typed from the callee's signature:
  `httpx.OK(c, newTokenResponse(t))` documents `TokenResponse`. Building the
  body with a constructor rather than a composite literal is how it is normally
  written, and it used to yield no response type at all.
- A status code the scanner cannot resolve is no longer reported as 200. It
  becomes OpenAPI's `default` response, so a handler that answers 201 is never
  documented as answering 200 — and the linter no longer advises "consider 201
  Created" about work already done.

- `examples/shop/shoppb` is regenerated with protoc-gen-go v1.36.11. The
  checked-in stubs panicked at package init against the protobuf runtime the
  module requires, which took the example's tests and the root package's tests
  down with them.

### Security
- The console no longer claims a documented path because it ends the same way
  as one of its own endpoints. A real `GET /v1/documents/{id}/source` was
  answered by the console's source reader, and `GET /v1/exports/openapi.json`
  served the whole specification to whoever asked. Endpoints are now matched
  exactly, against the mount point.
- `grpc/invoke` and `grpc/stream` are closed in production unless an
  `accessKey` is set. They dial a host named in the request body, so an open
  console let anyone who could reach it open connections from the server to
  internal addresses, cloud metadata services and closed ports.
- The access-key cookie is scoped to the console's mount point instead of `/`,
  so a deployment secret stops being attached to every request the application
  serves. It is also marked `Secure` when `X-Forwarded-Proto` is `https`, which
  is the case `r.TLS` misses on every TLS-terminating proxy.

### Added
- `c.DefaultQuery("limit", "20")` puts the fallback in the parameter schema as
  `default`.
- Build tags for the two dependencies that dominated install size. The default
  build is 7 modules instead of 28: `-tags grpclive` restores live gRPC calling
  from the console (grpcurl, grpc-go, protoreflect, go-spiffe, envoy protos),
  `-tags mcp` restores `spector -mcp`. Scanning `.proto` and writing
  `grpc.json` are unaffected and always built.

### Changed
- `spector.Handler` rescans when the source changes instead of caching the
  first scan for the life of the process, and no longer caches a failed scan —
  a console that 500'd on a half-written file kept doing so after the file was
  fixed. The tree is fingerprinted at most once a second.
- `mount` registers `synth/body` and `grpc/stream`, which the console fetches.
  gin and echo register each endpoint by name, so those two features were
  missing there while working on every framework mounted as a subtree.
- `Adapter` implementations parse through `astutil.ParseDir`, which walks
  recursively, skips `_test.go` and vendor/testdata trees, and skips a file
  that does not parse rather than failing the whole scan.
- `Operation.SetResponse(0, …)` files a response under `default`.

## [0.2.0] - 2026-08-12

### Added
- `-gateway` (`spector.GenerateGateway`): build a REST OpenAPI document from the
  `google.api.http` annotations in `.proto` sources — methods, path templates,
  `body` mappings, `additional_bindings` and `custom` kinds. Unannotated RPCs
  are left out; server-streaming bindings are marked `x-spector-realtime`.
- `-format yaml`: emit the OpenAPI, gRPC or GraphQL document as YAML instead of
  JSON, in the document's own key order. An `-o` ending in `.yaml`/`.yml`
  implies it; an explicit `-format` wins. `-all` writes the `.yaml` names.
- Manual route supplements: a `routes:` list in `spector.json`/`spector.yaml`
  (or `Config.Routes`) declares operations for routes the AST cannot resolve.
  They are folded into the document marked `x-spector-manual`, never override
  the scan, and a `fills: file.go:line` entry clears the diagnostic it answers
  so a supplemented codebase passes `-strict-routes`.
- Static route resolution: route paths and group prefixes built from
  package-level string `const`/`var` and `+` concatenation are resolved across
  all router adapters, not just string literals.
- `-strict-routes`: exit non-zero when a genuinely dynamic route
  (loop/slice/map/function-return) cannot be resolved statically. Such routes
  otherwise emit a diagnostic to stderr.
- Interactive gRPC streaming console: a WebSocket-backed session panel
  (Connect / Send / Half-close / Cancel) for all four method kinds, auth and
  same-origin gated.
- Router adapters: `httprouter`, `bunrouter` (in addition to gin, chi, echo,
  fiber, gorillamux, stdlib).
- Exports: AsyncAPI 2.6 (`-asyncapi`), HAR 1.2 (`-har`), and Postman collection
  enrichment (variables, environments, test scripts, examples).

### Fixed
- A label no longer hides a route prefix: a `LabeledStmt` wrapping a top-level
  `:=` was treated as a nested-block declaration and masked, producing a
  false-positive dynamic-route diagnostic.

### Changed
- `Adapter.Scan` returns `[]core.Diagnostic` alongside routes and schemas so
  unresolved-route reporting is uniform across adapters.

## [0.1.0]

Initial public baseline: zero-config OpenAPI generation from Go router source,
a browser console, mock and verifying-proxy modes, and typed client SDKs.

[Unreleased]: https://github.com/bakhod1r/spector/compare/v0.5.1...HEAD
[0.5.1]: https://github.com/bakhod1r/spector/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/bakhod1r/spector/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/bakhod1r/spector/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/bakhod1r/spector/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/bakhod1r/spector/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/bakhod1r/spector/releases/tag/v0.1.0
