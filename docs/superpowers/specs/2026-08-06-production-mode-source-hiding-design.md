# Production mode — hide API source — design

## Problem

The console exposes the scanned application's Go source. Each operation carries
an `x-specter-source` extension (file + line), the UI renders a **View source**
button from it, and the `source` HTTP endpoint (`specter.go`, the
`strings.HasSuffix(r.URL.Path, "source")` branch) reads and returns the actual
file contents via `source.Read`. On an internet-facing deployment this leaks
source code, file paths, and internal structure.

Confirmed live: `GET /docs/source?file=main.go&line=1` returns `package main …`.

## Goal

A production mode that hides API source from the console: no source content, no
file paths, no line numbers, no button. Default (unset) keeps today's
source-viewing developer experience unchanged. Non-breaking.

## Scope (approved: focused, named `Production` as the extensible seam)

`Production` is a general hardening flag, but this spec wires it to exactly one
behaviour: hiding source. It is named `Production` (not `HideSource`) so future
internet-facing hardening (e.g. gating `grpc/invoke`, stripping the
`x-specter-calls` call graph) can attach to the same flag later. Those are
**out of scope** here — this change strips source only.

## Design

### Config + flag

- Add `Config.Production bool` to `specter.Config` (documented alongside
  `AccessKey`, since both concern internet-facing deployment).
- Add CLI flag `-prod` in `cmd/specter/main.go` that sets `cfg.Production`.
- Add `production` to the JSON config-file struct (`fileConfig`) and copy it into
  `cfg.Production` in `applyConfigFile`, matching how `AccessKey`/`BasePath` are
  read, so one file can describe a production deployment.

### Two enforcement points (defence in depth)

1. **Strip source from the document (primary).** In `Generate` (`specter.go`),
   after the document is built, if `cfg.Production` is set, walk every operation
   in `doc.Paths` and set `Operation.Source = nil`. Because the field is
   `json:"x-specter-source,omitempty"`, the extension then disappears from the
   emitted JSON. Consequences:
   - The UI's **View source** button is gated on `op["x-specter-source"]`
     (`ui.html:1573`); with the extension gone, the button never renders.
   - File paths and line numbers no longer appear anywhere in the document.

   This is enforced in `Generate`, so it also protects `-o` file output and any
   other consumer of the document, not just the console.

2. **Refuse the source endpoint (secondary).** In the console `Handler`, when
   `cfg.Production` is set, the `source` branch returns
   `http.Error(w, "not available", http.StatusNotFound)` before calling
   `source.Read`, so a hand-crafted request cannot retrieve a file even though
   the button is gone. The UI already handles a non-OK `source` response by
   showing "source not available on this server" (`ui.html:1689`), so no UI
   change is required beyond the button disappearing.

### Data flow

`-prod` / config `production` → `cfg.Production` → (a) `Generate` nils each
`Operation.Source` so the document and console omit it; (b) `Handler` 404s the
`source` endpoint. No change to `core` types, the `Adapter` interface, or the
document schema (the field is simply absent when nil).

### Error handling

- Stripping source is a plain field-nil over `doc.Paths`; no failure path.
- The endpoint guard returns the same 404 the UI already treats as
  "not available" — no new UI state.

## Testing (TDD)

- `Generate` unit: with `Config.Production` true, every operation's `Source` is
  nil and the marshalled JSON contains no `x-specter-source`; with it false (a
  control), at least one operation still has `Source` set. Uses `examples/shop`.
- `Handler` unit: with `Production` true, `GET /docs/source?file=…&line=…`
  returns 404; with it false, it returns the snippet JSON (guard the existing
  behaviour against regression).
- Config-file unit: a config with `"production": true` sets `cfg.Production`
  (mirror the existing `AccessKey`/`BasePath` config-file test if present).
- Full suite `go test ./...` stays green.

## Non-goals

- Gating `grpc/invoke`, `synth/body`, or the mock in production (future work on
  the same flag).
- Stripping `x-specter-calls` / `x-specter-middleware` / `x-specter-advice`
  (call graph, middleware, advisories) — a separate decision; this change is
  scoped to source.
- Any authentication change; `AccessKey` remains the access-gating mechanism.
