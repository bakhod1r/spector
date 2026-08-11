# Run All — show request & response per row — design

## Problem

The console's **Run all** runs every operation in the active tab and reports
PASS/FAIL, status, latency, and (on failure) the mismatch reasons. It does not
show the request that was sent or the response body, so a user cannot see *what*
was sent or *why* a body failed beyond the terse reasons. The data already
exists inside each runner (`runOneRest` and the inline gRPC/GraphQL loops in
`internal/ui/ui.html`) — it is built, used for the pass/fail check, and then
discarded.

## Goal

Let a user open any Run All result row and see the exact request that was sent
and the response that came back, across all three tabs (REST, gRPC, GraphQL).
Purely a console (ui.html) change; no Go API or document change.

## Scope (approved)

- All three runners: REST, gRPC, GraphQL.
- Each row is click-to-expand, **collapsed by default** (a run can have many
  rows; the summary view must stay scannable).
- Show both request and response. Bodies are shown as-is (this is the user's own
  local console showing their own request) — no auth/header masking — but are
  truncated past a size cap so one huge body cannot swamp the modal.
- JSON bodies are pretty-printed when parseable; otherwise shown verbatim.

Out of scope: exporting/copying the request (a separate feature; the existing
"Copy as cURL" on the single-request view already covers that need), and any
change to what counts as PASS/FAIL.

## Design

### Result shape

Each runner already returns (or passes to `addRunRow`) a row object
`{ label, status, ms, pass, reasons }`. Add one optional field:

```
detail: {
  request:  { title: string, meta?: {k:v}, body?: string },
  response: { status: number|null, body?: string },
}
```

- `title` is the request's headline: `"GET https://…/widgets"` (REST),
  `"host:port  package.Service/Method"` (gRPC), `"POST https://…/graphql"`
  (GraphQL).
- `meta` is a small key→value map rendered as a list: REST request headers;
  gRPC `symbol` + `target`; GraphQL `variables` (JSON). Omitted when empty.
- `request.body` / `response.body` are strings already captured in the runner
  (the interpolated request body / GraphQL query / gRPC `data`, and the raw
  response `text`). Omitted when empty.

`detail` is optional: a row built on the error path (a thrown exception before a
response) may carry only `request`, or omit `detail` entirely, and the row still
renders exactly as today.

### Capturing the data (three runners)

- **`runOneRest`** already has `url`, `opts` (method + headers), the interpolated
  `req.body`, and the response `text`. Return `detail` with
  `request: { title: METHOD+" "+url, meta: opts.headers, body: reqBody }` and
  `response: { status: res.status, body: text }`.
- **`runAllGrpc`** inline loop already has `target`, `svc.fullName+"/"+m.name`,
  `data`, and response `text`. Build the same `detail` and pass it into the row
  object handed to `addRunRow`.
- **`runAllGraphql`** inline loop has `url`, the query string, the variables, and
  the response `text`. Same treatment.

To avoid three copies of the formatting, add one helper
`makeDetail(request, response)` that normalizes/omits empty fields; each runner
calls it.

### Rendering (`addRunRow`)

- When `r.detail` is present, prepend a chevron (`▶`) to the run line and give
  the line `cursor:pointer` + a toggle handler; without `detail` the row is
  unchanged (no chevron, not clickable).
- Clicking toggles a `.run-detail` panel appended to the row and flips the
  chevron `▶`/`▼`. The panel has a **Request** block (title, a `meta` key/value
  list, a `<pre>` body) and a **Response** block (status line, a `<pre>` body).
- `renderBody(str)`: if `JSON.parse` succeeds, pretty-print with 2-space indent;
  else show verbatim. If the string exceeds `RUN_BODY_CAP` (4000 chars), show
  the first `RUN_BODY_CAP` and append `"\n… (N more characters)"`.
- All body/title/meta text is inserted via `textContent` / DOM nodes, never
  `innerHTML`, so a response body cannot inject markup into the console.

### CSS

Add `.run-detail`, `.run-detail h4`, `.run-kv`, and a `.run-chev` rule reusing
the existing modal/panel tokens (`--panel`, `--border`, `--muted`, `--code-bg`).
Bodies use the existing monospace `<pre>` treatment already used elsewhere in
the console.

## Error handling

- No response (thrown before fetch resolves): `detail.response` omitted or
  `status: null`; the Request block still shows what was about to be sent when
  available.
- Non-JSON body: shown verbatim by `renderBody`.
- Oversized body: truncated with a visible count; the run itself is unaffected
  (truncation is display-only).

## Testing

- **`internal/ui/ui_test.go`** (Go contract): assert the page contains the new
  toggle machinery so a rename can't silently break it — e.g. the `run-detail`
  class, the `renderBody` and `makeDetail` function names, and `RUN_BODY_CAP`.
  These are `strings.Contains` checks, matching the existing ui_test style.
- **`e2e/console.spec.js`** (Playwright, runs in CI's Browser job): against the
  example console, click **Run all** on REST, wait for rows, click the first
  row, and assert a request title and a response body become visible. This is
  the behavioural test; it exercises the real toggle and capture path.
- Full `go test ./...` stays green (the ui_test contract additions compile and
  pass); the e2e suite stays green.

## Non-goals

- Masking or redacting request headers/bodies.
- Persisting or exporting run details.
- Any change to PASS/FAIL logic, the summary counts, or the single-request view.
