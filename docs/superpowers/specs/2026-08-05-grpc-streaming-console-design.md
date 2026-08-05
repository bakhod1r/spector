# Interactive gRPC Streaming Console — Design

Date: 2026-08-05
Status: Approved (design)

## Problem

The console (`internal/ui/ui.html` served by `specter.Handler` in `specter.go`)
can already invoke gRPC methods, but only in a **batch, one-shot** way: the UI
collects every request message, POSTs them together to the `grpc/invoke`
endpoint, and `grpcx.Invoke` runs `grpcurl.InvokeRPC`, buffering every response
and returning them all at once.

This is not interactive. A caller cannot:

- send request messages one at a time and watch server responses arrive live,
- see server-streaming messages appear incrementally as they are produced,
- half-close a client-stream / bidi stream and then observe the reply,
- cancel an in-flight stream.

The whole point of client-streaming and bidirectional streaming is live,
incremental interaction, which the current one-shot request/response model
cannot express.

## Goal

Make all four gRPC method kinds (unary, server-stream, client-stream, bidi)
invocable through a **live, interactive** session in the console, over a
WebSocket, so messages can be sent incrementally and responses stream back in
real time.

## Non-goals

- No change to the `-grpc` document generation or `.proto`/`.pb.go` output.
- No change to the existing `grpc/invoke` one-shot endpoint; the "run all"
  batch runner (`ui.html`, ~line 2809) keeps using it.
- No new external dependency: `github.com/gorilla/websocket` is already in
  `go.mod`.

## Architecture

### Transport

A new WebSocket endpoint. `specter.Handler` (in `specter.go`) already routes by
path suffix; add a branch for a path ending in `grpc/stream` that upgrades the
connection to a WebSocket. Each WebSocket connection carries exactly one RPC
invocation.

### Backend: `internal/grpcx`

New function alongside `Invoke`:

```go
func Stream(protoDir string, conn *websocket.Conn, req Request) error
```

`req` is the existing `grpcx.Request` shape (target, symbol, headers, tls,
insecure, timeoutSec); it is read from the first WebSocket frame (an `init`
frame) or from query params — see Frame protocol below.

Internals, reusing the existing grpcurl plumbing so no manual proto reflection
is added:

- Dial + `descriptorSource` exactly as `Invoke` does today.
- `io.Pipe()`. The WebSocket read loop, on a `send` frame, writes the frame's
  JSON `data` followed by a newline into the pipe writer.
- `grpcurl.RequestParserAndFormatter(FormatJSON, source, pr, ...)` reads the
  pipe: each `rf.Next` call parses exactly one JSON message. This is the same
  parser used by `Invoke`, so message parsing behaviour is identical.
- A `halfClose` frame closes the pipe writer, so the next `rf.Next` returns
  `io.EOF` and grpcurl stops sending requests (half-close).
- A `cancel` frame cancels the invocation `context`.
- A custom `InvocationEventHandler`:
  - `OnReceiveResponse(msg)` → format with the formatter → emit a `message`
    frame.
  - `OnReceiveHeaders` → optional; may be dropped for v1.
  - `OnReceiveTrailers(status, md)` → emit a `status` frame (code, message).
  - errors during invoke → emit an `error` frame.
- Concurrency: one dedicated **writer goroutine** owns all WebSocket writes
  (gorilla/websocket forbids concurrent writers). `InvokeRPC` runs in its own
  goroutine (it blocks). The read loop runs on the calling goroutine. Frames to
  send are funnelled through a channel to the writer goroutine.
- Timeout: `req.TimeoutSec` (0 → default 15s, matching `Invoke`) via a
  `context.WithTimeout`.
- On invoke completion (success, error, EOF, cancel, or timeout) the backend
  emits `end` and closes the socket.

### Frame protocol (JSON, both directions)

Client → server:

| type        | fields         | meaning                                    |
|-------------|----------------|--------------------------------------------|
| `init`      | Request fields | start the RPC (if not passed via query)    |
| `send`      | `data` (JSON)  | one request message                        |
| `halfClose` | —              | no more request messages                   |
| `cancel`    | —              | cancel the RPC                             |

Server → client:

| type      | fields             | meaning                                  |
|-----------|--------------------|------------------------------------------|
| `open`    | —                  | stream established, ready to send        |
| `message` | `data` (JSON)      | one response message                     |
| `status`  | `code`, `message`  | final gRPC status                        |
| `error`   | `error`            | transport/parse/invoke error             |
| `end`     | —                  | terminal; socket about to close          |

### Method-kind behaviour in the UI

All four kinds use the WebSocket path. The method kind drives which controls are
enabled:

- **unary**: send one message; the UI auto-sends `halfClose` after the single
  `send`. One `message` + `status` expected.
- **server-stream**: send one message, auto `halfClose`; many `message` frames
  stream in.
- **client-stream**: **Send** enabled repeatedly; user clicks **Half-close** to
  finish; one `message` + `status`.
- **bidi**: **Send** repeatedly and **Half-close** independently; `message`
  frames may interleave with sends.

### Frontend: `internal/ui/ui.html`

In the per-method gRPC panel (`renderGrpc` / method card builder):

- Replace the batch `invokeGrpc` one-shot flow with a **live session** panel:
  - **Connect** button → opens the `grpc/stream` WebSocket; on `open` frame,
    enables the send controls.
  - Request message textarea + **Send** button (repeatable for client/bidi;
    single-shot then auto half-close for unary/server-stream).
  - **Half-close** and **Cancel** buttons.
  - A live response log rendered in the style of the realtime tab's `ctx.say`
    message log: each `message` frame appended incrementally; `status`/`error`
    close out the log; `end` marks the session done.
- The existing kind badges (`b-unary`/`b-server`/`b-client`/`b-bidi`) gate which
  buttons are active.
- **Copy as grpcurl** stays.
- Interpolation of `{{var}}` in target and message bodies stays (as in current
  `invokeGrpc`).

## Error handling

- WebSocket upgrade is gated by the same `authorized(r, cfg.AccessKey)` check as
  every other console route, evaluated **before** upgrade; unauthorized → 404
  (consistent with the rest of `Handler`, which 404s rather than 401s a gated
  console). Same-origin is enforced via the gorilla upgrader `CheckOrigin`.
- Dial / descriptor / parse errors → `error` frame then `end`.
- gRPC non-OK status → `status` frame (non-terminal for the socket) then `end`.
- Malformed client frame → `error` frame; the session may continue or end
  depending on severity (parse of a `send` payload → `error`, keep open; bad
  control frame → `error` + `end`).
- Client disconnect → cancel the context, tear down the invoke goroutine, close
  the pipe.

## Security

- Auth gate before upgrade (above).
- `CheckOrigin` restricts the WebSocket to same-origin.
- The console already documents that it can invoke the target's gRPC methods
  (`specter.go` comment near `AccessKey`); this feature does not widen that
  surface beyond what `grpc/invoke` already exposes — it only makes the existing
  capability interactive.

## Testing

- `internal/grpcx` (new streaming test file): spin up an in-process gRPC test
  server implementing a server-stream, a client-stream, and a bidi echo method;
  drive `Stream` over a real `websocket` pair (`httptest` server) and assert:
  - server-stream: one send + halfClose → N `message` frames + `status` + `end`.
  - client-stream: several sends + halfClose → one `message` + `status`.
  - bidi echo: interleaved send/receive ordering.
  - `cancel` frame terminates an in-flight stream.
  - unauthorized upgrade → 404.
- `internal/ui/ui_test.go`: add the new control ids to `TestControlsHaveMarkup`;
  add a contract test that the page opens a WebSocket to `grpc/stream` and
  references the frame `type` constants.
- `specter.go` handler test: WS upgrade rejected without the access key when one
  is configured.
- Full suite: `go test ./...` green.

## Workflow

Per the user's standing preference: build on a branch with TDD, `go test ./...`,
commit, then merge `--no-ff` to main and delete the branch on their approval.
This is the first of two sequential backlog items; the second (dynamic /
non-literal route inference) follows in its own spec → plan → implementation
cycle.
