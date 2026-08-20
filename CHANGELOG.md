# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
  `-tags mcp` restores `specter -mcp`. Scanning `.proto` and writing
  `grpc.json` are unaffected and always built.

### Changed
- `specter.Handler` rescans when the source changes instead of caching the
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
- `-gateway` (`specter.GenerateGateway`): build a REST OpenAPI document from the
  `google.api.http` annotations in `.proto` sources — methods, path templates,
  `body` mappings, `additional_bindings` and `custom` kinds. Unannotated RPCs
  are left out; server-streaming bindings are marked `x-specter-realtime`.
- `-format yaml`: emit the OpenAPI, gRPC or GraphQL document as YAML instead of
  JSON, in the document's own key order. An `-o` ending in `.yaml`/`.yml`
  implies it; an explicit `-format` wins. `-all` writes the `.yaml` names.
- Manual route supplements: a `routes:` list in `specter.json`/`specter.yaml`
  (or `Config.Routes`) declares operations for routes the AST cannot resolve.
  They are folded into the document marked `x-specter-manual`, never override
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

[Unreleased]: https://github.com/bakhod1r/spector/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/bakhod1r/spector/compare/v0.3.0...v0.4.0
[0.2.0]: https://github.com/bakhod1r/spector/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/bakhod1r/spector/releases/tag/v0.1.0
