# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- `-gateway`: an RPC with `additional_bindings` gave every binding the same
  `operationId`, which OpenAPI requires to be unique. The first binding keeps
  the RPC's name; the rest are numbered.
- Manual route supplements: a response whose status code is not a number
  ("default", "4XX") was emitted with an empty `description`, which OpenAPI
  forbids.
- Manual route supplements: `fills` never matched when the scan reported a bare
  filename — what `-dir .` produces, the most common invocation — because the
  match required a directory separator.

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

[Unreleased]: https://github.com/bakhod1r/spector/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/bakhod1r/spector/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/bakhod1r/spector/releases/tag/v0.1.0
