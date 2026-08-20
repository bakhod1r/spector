# Dynamic route inference 2b — merge observed traffic — design

## Problem

Routes the AST cannot statically resolve (loop/slice/map/function-return paths)
are absent from the generated document and emit a `core.Diagnostic`. The
`-proxy-learn` machinery already observes live traffic and writes an OpenAPI
*fragment* for endpoints seen but not documented (`internal/proxy/learn.go`,
`Learner.Observe`/`Document`). But that fragment is a **separate** file: nothing
folds the learned routes back into the source document to fill the gaps, and a
user must merge two files by hand.

## Goal

Fill the dynamic-route gaps (and enrich documented operations) from observed
traffic by merging the learned observations into the source document, producing
one combined document. Two front doors — live during a proxy run, and offline
from an existing fragment — over one shared merge function.

## Scope (approved)

- One merge function, two front doors (`-proxy-merge`, `-merge-learned`).
- Merge rule: gap-fill **and** status enrichment.
  - An observed `(method, path)` absent from the base document is added whole,
    marked `x-spector-observed`.
  - An observed `(method, path)` present in the base has its **new** observed
    status codes appended; existing responses are never overwritten (the source
    spec wins per response).
- Dedup key: `(method, normalised path)`. The base always wins on conflict.

Out of scope: matching a learned path to a specific diagnostic's source location
(the diagnostic only knows a file:line, not a path — merge works on
documented-vs-observed, and the diagnostics are the user-facing motivation);
schema reconciliation between a source response and an observed one for the same
status (source wins, observed is dropped for that status).

## Architecture — one shared merge function

Add to `internal/proxy`:

```go
// MergeObserved returns base with observed traffic folded in: operations the
// base lacks are added and marked observed; operations the base has gain only
// the status codes it did not already document. base is not mutated.
func MergeObserved(base, observed *core.Document) *core.Document
```

Exposed as `spector.MergeObserved` (alias/wrapper in `spector.go`, matching how
`MockHandler`/`ServeMock` are surfaced).

Algorithm:

1. Deep-copy `base` into `out` (so neither input is mutated).
2. For each `(path, method, op)` in `observed` (iterate paths then methods in
   sorted order for deterministic output):
   - If `out.Paths[path][method]` exists (dedup hit): for each response status
     in the observed op **not** already in the base op's `Responses`, add it
     (carrying the Learner's `"Observed N time(s)"` description and inferred
     schema). Never replace an existing response.
   - Else: add the whole observed op under `(path, method)` and set
     `op.Observed = true`.
3. Return `out`.

Path identity is the exact template string as it appears in each document; the
Learner already normalises request paths to templates (`NormalisePath`) and the
base uses source templates, so a documented `/users/{id}` and an observed
`/users/{id}` are the same key. No re-normalisation is done inside the merge.

## core.Operation change

Add one field to `core.Operation`:

```go
// Observed marks an operation that came from observed traffic (via merge),
// not from source. "x-" makes it a vendor extension consumers ignore.
Observed bool `json:"x-spector-observed,omitempty"`
```

`omitempty` keeps every existing document byte-identical.

## Front door 1 — live during a proxy run

- New bool flag `-proxy-merge` in `cmd/spector/main.go`, threaded into
  `proxyConfig`.
- In `runProxy` (`cmd/spector/proxy.go`), the learner-write branch: when
  `-proxy-merge` is set, write `spector.MergeObserved(doc, learner.Document(...))`
  to the `-proxy-learn` file instead of the bare fragment (`doc` is the scanned
  source document `runProxy` already holds). The stderr message says a merged
  document was written rather than a fragment.
- `-proxy-merge` without `-proxy-learn` is a usage error (nothing names the
  output file): report it and exit non-zero.

## Front door 2 — offline from a fragment

- New flag `-merge-learned <fragment.json>` in `cmd/spector/main.go`.
- When set, after the base document is generated from `-dir` (the normal scan
  path), read and JSON-decode the fragment file into a `*core.Document`, compute
  `spector.MergeObserved(base, fragment)`, and write it to `-o` (or stdout) using
  the same output path the normal document takes. A missing/!JSON fragment is a
  fatal error naming the file.
- It composes with nothing else exotic: it is a post-scan transform of the base
  document, so `-title`/`-version`/`-o` behave as usual.

## Data flow

`-proxy` + `-proxy-learn f` + `-proxy-merge`: scan → base doc → proxy observes →
on exit `MergeObserved(base, learner.Document())` → `f`.

`-dir` + `-merge-learned frag.json` + `-o out`: scan → base doc → read `frag` →
`MergeObserved(base, frag)` → `out`.

## Error handling

- `MergeObserved` never mutates its inputs and never panics: an observed op with
  no responses still adds a bare op; a nil `observed` returns a copy of base.
- Proxy: `-proxy-merge` without `-proxy-learn` → clear usage error.
- Offline: unreadable or non-JSON `-merge-learned` file → fatal error naming the
  file and the decode problem.

## Testing (TDD)

- `internal/proxy` unit (`MergeObserved`):
  - Gap-fill: an observed op absent from base is added with `Observed == true`.
  - Enrich: an observed status code absent from a documented op is appended; an
    existing response for a status the base already documents is **unchanged**
    (source wins).
  - Dedup: identical `(method, path)` present in both does not duplicate the op.
  - Immutability: base and observed are unchanged after the call.
- `cmd/spector` CLI:
  - `-merge-learned`: `writeTree` a small source + a fragment JSON with one
    undocumented path and one extra status on a documented path; assert stdout
    contains the observed path, `x-spector-observed`, and the enriched status.
  - `-proxy-merge` without `-proxy-learn` exits non-zero.
- Proxy exit path: extend an existing `proxy_test`/`runProxy` test (or add one)
  to assert the merged file contains both a documented and an observed route when
  `-proxy-merge` is set. If driving the live proxy is impractical in a unit test,
  cover the write decision by asserting `MergeObserved` output is what gets
  marshalled (the write branch is a thin call around the tested function).
- Full `go test ./...` stays green.

## Non-goals

- Reconciling conflicting schemas for the same status between source and traffic.
- Persisting learned data across proxy runs, or deduping across multiple
  fragments.
- Any change to the existing `-proxy-learn` fragment output when `-proxy-merge`
  is not set (fully backward compatible).
