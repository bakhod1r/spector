# Interactive gRPC Streaming Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make all four gRPC method kinds (unary, server-stream, client-stream, bidi) invocable through a live, interactive WebSocket session in the console, so request messages are sent incrementally and responses stream back in real time.

**Architecture:** A new `grpcx.Stream` function drives one RPC per WebSocket connection. It reuses grpcurl's existing JSON request parser by feeding it an `io.Pipe`: WebSocket `send` frames write JSON messages into the pipe, `halfClose` closes the writer (→ `io.EOF`), and a custom `InvocationEventHandler` pushes each received response back over the socket. `spector.Handler` upgrades requests whose path ends in `grpc/stream`. The console's per-method gRPC panel becomes a live session with Connect / Send / Half-close / Cancel and an incremental response log.

**Tech Stack:** Go, `github.com/fullstorydev/grpcurl` v1.9.3, `github.com/gorilla/websocket` v1.5.3, `google.golang.org/grpc`, the shop example proto (`examples/shop/proto/shop.proto` → `examples/shop/shoppb`), single embedded HTML console (`internal/ui/ui.html`).

## Global Constraints

- No new external dependencies; everything above is already in `go.mod`.
- Module path is `github.com/bakhod1r/spector`.
- The console 404s (never 401s) unauthorized requests — a gated console must not confirm it exists. Every new route follows this.
- gRPC symbol format from the UI is `package.Service/Method`; `Invoke`/`Stream` normalise the slash. Proto package is `shop.v1`.
- `Request` JSON shape (existing, in `internal/grpcx/invoke.go`): `target`, `symbol`, `data`, `headers`, `tls`, `insecure`, `timeoutSec` (0 → 15s default).
- Existing `grpc/invoke` one-shot endpoint and the console "run all" batch runner that uses it stay untouched.
- `proto.Message` in grpcurl callbacks is `github.com/golang/protobuf/proto.Message`; `desc.MethodDescriptor` is `github.com/jhump/protoreflect/desc`.

## File Structure

- `examples/shop/proto/shop.proto` — add one bidi method `Chat(stream GetUserRequest) returns (stream User)` to `UserService` so tests have a bidi target. Regenerate `shoppb`.
- `examples/shop/shoppb/shop.pb.go`, `examples/shop/shoppb/shop_grpc.pb.go` — regenerated.
- `examples/shop/grpcserver.go` — implement the new `Chat` bidi handler on the existing server type (echoes each request's id back as a `User`).
- `internal/grpcx/stream.go` — new: frame types, `Stream`, the pipe-backed request flow, the custom event handler, the writer goroutine.
- `internal/grpcx/stream_test.go` — new: WebSocket-level integration tests using a real gRPC test server and a real `httptest` WebSocket endpoint.
- `internal/grpcx/live_test.go` — add a `Chat` bidi handler to the in-test `userServer` so the streaming tests have a bidi method (mirror of the example server handler).
- `spector.go` — add the `grpc/stream` branch to `Handler`: auth gate, upgrade, call `grpcx.Stream`.
- `spector_grpc_stream_test.go` (or extend an existing handler test file) — the upgrade auth-gate test.
- `internal/ui/ui.html` — replace the batch invoke flow in the gRPC method panel with a live session; add controls.
- `internal/ui/ui_test.go` — pin the new control ids and the `grpc/stream` contract.

---

### Task 1: Add a bidi method to the shop example and regenerate

**Files:**
- Modify: `examples/shop/proto/shop.proto:56` (inside `service UserService`)
- Modify: `examples/shop/grpcserver.go`
- Regenerate: `examples/shop/shoppb/shop.pb.go`, `examples/shop/shoppb/shop_grpc.pb.go`

**Interfaces:**
- Produces: gRPC method `shop.v1.UserService/Chat`, client-and-server streaming, request `GetUserRequest`, response `User`. Server echoes each received request as `User{Id: req.Id, Name: "echo"}`.

- [ ] **Step 1: Add the RPC to the proto**

In `examples/shop/proto/shop.proto`, inside `service UserService { ... }` (after the `CountUsers` line ~56), add:

```proto
  rpc Chat(stream GetUserRequest) returns (stream User);
```

- [ ] **Step 2: Regenerate the Go bindings**

Run from repo root:

```bash
protoc -I examples/shop/proto \
  --go_out=. --go_opt=module=github.com/bakhod1r/spector \
  --go-grpc_out=. --go-grpc_opt=module=github.com/bakhod1r/spector \
  examples/shop/proto/shop.proto
```

Expected: `examples/shop/shoppb/shop.pb.go` and `shop_grpc.pb.go` change; a new `UserService_ChatServer`/`UserService_ChatClient` interface appears in `shop_grpc.pb.go`.

- [ ] **Step 3: Implement the bidi handler on the example server**

In `examples/shop/grpcserver.go`, add a method on the same receiver type that implements the other `UserService` handlers (find it with `grep -n "func (.*) GetUser" examples/shop/grpcserver.go` and reuse that receiver):

```go
func (s *userService) Chat(stream shoppb.UserService_ChatServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(&shoppb.User{Id: req.Id, Name: "echo"}); err != nil {
			return err
		}
	}
}
```

Ensure `io` is imported in that file.

- [ ] **Step 4: Build the example**

Run: `go build ./examples/...`
Expected: PASS (compiles).

- [ ] **Step 5: Commit**

```bash
git add examples/shop/proto/shop.proto examples/shop/shoppb examples/shop/grpcserver.go
git commit -m "feat(example): add Chat bidi method to shop UserService"
```

---

### Task 2: Frame types and unary streaming over WebSocket

**Files:**
- Create: `internal/grpcx/stream.go`
- Create: `internal/grpcx/stream_test.go`
- Modify: `internal/grpcx/live_test.go` (add `Chat` handler to the in-test server)

**Interfaces:**
- Produces:
  - `func Stream(protoDir string, conn *websocket.Conn) error` — reads an `init` frame carrying a `Request`, dials, invokes, and streams frames until the RPC ends or the socket closes.
  - Client → server frames (JSON): `{"type":"init","request":{...Request...}}`, `{"type":"send","data":<json>}`, `{"type":"halfClose"}`, `{"type":"cancel"}`.
  - Server → client frames (JSON): `{"type":"open"}`, `{"type":"message","data":<json>}`, `{"type":"status","code":"OK","message":""}`, `{"type":"error","error":"..."}`, `{"type":"end"}`.

- [ ] **Step 1: Add the `Chat` handler to the in-test server**

In `internal/grpcx/live_test.go`, on the existing `userServer` type, add (mirrors the example, needed so streaming tests have a bidi target):

```go
func (s *userServer) Chat(stream shoppb.UserService_ChatServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(&shoppb.User{Id: req.Id, Name: "echo"}); err != nil {
			return err
		}
	}
}
```

- [ ] **Step 2: Write the failing unary streaming test**

Create `internal/grpcx/stream_test.go`. This test starts the gRPC server (via `startServer` from `live_test.go`), stands up an `httptest` server whose handler upgrades to a WebSocket and calls `Stream`, dials it as a client, drives a unary call, and collects frames.

```go
package grpcx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// streamServer wraps Stream in an httptest server that upgrades to WebSocket.
func streamServer(t *testing.T) *httptest.Server {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		_ = Stream(protoDir, conn)
	}))
}

// dialWS opens a client WebSocket to an httptest server URL.
func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return c
}

// collect reads frames until "end" (or the socket closes) and returns them.
func collect(t *testing.T, c *websocket.Conn) []map[string]json.RawMessage {
	t.Helper()
	var frames []map[string]json.RawMessage
	for {
		var f map[string]json.RawMessage
		if err := c.ReadJSON(&f); err != nil {
			return frames
		}
		frames = append(frames, f)
		var typ string
		json.Unmarshal(f["type"], &typ)
		if typ == "end" {
			return frames
		}
	}
}

func typesOf(frames []map[string]json.RawMessage) []string {
	var out []string
	for _, f := range frames {
		var typ string
		json.Unmarshal(f["type"], &typ)
		out = append(out, typ)
	}
	return out
}

func TestStreamUnary(t *testing.T) {
	target := startServer(t, false)
	srv := streamServer(t)
	defer srv.Close()
	c := dialWS(t, srv)
	defer c.Close()

	c.WriteJSON(map[string]any{"type": "init", "request": map[string]any{
		"target": target, "symbol": "shop.v1.UserService/GetUser",
	}})
	c.WriteJSON(map[string]any{"type": "send", "data": json.RawMessage(`{"id":7}`)})
	c.WriteJSON(map[string]any{"type": "halfClose"})

	frames := collect(t, c)
	got := typesOf(frames)
	// Expect an open, at least one message, a status, and an end.
	for _, want := range []string{"open", "message", "status", "end"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %q frame; got %v", want, got)
		}
	}
	// The message payload must carry the echoed id.
	var msgData string
	for _, f := range frames {
		var typ string
		json.Unmarshal(f["type"], &typ)
		if typ == "message" {
			msgData = string(f["data"])
		}
	}
	if !strings.Contains(msgData, `"id": 7`) && !strings.Contains(msgData, `"id":7`) {
		t.Errorf("message did not echo id 7; got %s", msgData)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/grpcx/ -run TestStreamUnary -v`
Expected: FAIL — `Stream` undefined (and `Chat` handler already added compiles).

- [ ] **Step 4: Implement `Stream` and the frame plumbing**

Create `internal/grpcx/stream.go`:

```go
package grpcx

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/fullstorydev/grpcurl"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/jhump/protoreflect/desc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type inFrame struct {
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data,omitempty"`
	Request *Request        `json:"request,omitempty"`
}

type outFrame struct {
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data,omitempty"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// wsHandler turns grpcurl invocation callbacks into outbound frames.
type wsHandler struct {
	format grpcurl.Formatter
	send   func(outFrame)
}

func (h *wsHandler) OnResolveMethod(*desc.MethodDescriptor) {}
func (h *wsHandler) OnSendHeaders(metadata.MD)              {}
func (h *wsHandler) OnReceiveHeaders(metadata.MD)           {}
func (h *wsHandler) OnReceiveResponse(m proto.Message) {
	s, err := h.format(m)
	if err != nil {
		h.send(outFrame{Type: "error", Error: err.Error()})
		return
	}
	h.send(outFrame{Type: "message", Data: json.RawMessage(s)})
}
func (h *wsHandler) OnReceiveTrailers(st *status.Status, _ metadata.MD) {
	h.send(outFrame{Type: "status", Code: st.Code().String(), Message: st.Message()})
}

// Stream drives one RPC over a WebSocket connection. It expects an init frame
// carrying the Request, then send/halfClose/cancel frames.
func Stream(protoDir string, conn *websocket.Conn) error {
	defer conn.Close()

	// First frame must be init.
	var initFrame inFrame
	if err := conn.ReadJSON(&initFrame); err != nil {
		return err
	}
	if initFrame.Type != "init" || initFrame.Request == nil {
		conn.WriteJSON(outFrame{Type: "error", Error: "first frame must be init with a request"})
		conn.WriteJSON(outFrame{Type: "end"})
		return nil
	}
	req := *initFrame.Request
	if req.Target == "" {
		conn.WriteJSON(outFrame{Type: "error", Error: "target is required"})
		conn.WriteJSON(outFrame{Type: "end"})
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutOf(req))
	defer cancel()

	cc, err := grpc.NewClient(req.Target, grpc.WithTransportCredentials(dialCreds(req)))
	if err != nil {
		conn.WriteJSON(outFrame{Type: "error", Error: err.Error()})
		conn.WriteJSON(outFrame{Type: "end"})
		return nil
	}
	defer cc.Close()

	source, err := descriptorSource(ctx, protoDir, cc)
	if err != nil {
		conn.WriteJSON(outFrame{Type: "error", Error: err.Error()})
		conn.WriteJSON(outFrame{Type: "end"})
		return nil
	}

	// The pipe feeds grpcurl's JSON request parser: each "send" frame writes one
	// JSON message; "halfClose" closes the writer so the parser sees io.EOF.
	pr, pw := io.Pipe()
	rf, formatter, err := grpcurl.RequestParserAndFormatter(
		grpcurl.FormatJSON, source, pr, grpcurl.FormatOptions{EmitJSONDefaultFields: true})
	if err != nil {
		conn.WriteJSON(outFrame{Type: "error", Error: err.Error()})
		conn.WriteJSON(outFrame{Type: "end"})
		return nil
	}

	// One writer goroutine owns all socket writes (gorilla forbids concurrent
	// writers). Frames are funnelled through sendCh.
	sendCh := make(chan outFrame, 16)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for f := range sendCh {
			if err := conn.WriteJSON(f); err != nil {
				return
			}
		}
	}()
	send := func(f outFrame) {
		defer func() { recover() }() // sendCh may be closed on teardown
		sendCh <- f
	}

	var headers []string
	for k, v := range req.Headers {
		headers = append(headers, k+": "+v)
	}
	symbol := strings.Replace(req.Symbol, "/", ".", 1)
	handler := &wsHandler{format: formatter, send: send}

	// Invoke runs in its own goroutine; it blocks pulling requests via rf.Next.
	invokeDone := make(chan error, 1)
	go func() {
		invokeDone <- grpcurl.InvokeRPC(ctx, source, cc, symbol, headers, handler, rf.Next)
	}()

	send(outFrame{Type: "open"})

	// Read loop: translate client frames into pipe writes / control actions.
	readErr := make(chan struct{})
	go func() {
		defer close(readErr)
		for {
			var f inFrame
			if err := conn.ReadJSON(&f); err != nil {
				cancel()
				return
			}
			switch f.Type {
			case "send":
				pw.Write(f.Data)
				pw.Write([]byte("\n"))
			case "halfClose":
				pw.Close()
			case "cancel":
				cancel()
				return
			}
		}
	}()

	// Wait for the invocation to finish (EOF, status, error, cancel, timeout).
	ierr := <-invokeDone
	pw.Close() // unblock any pending parser read
	if ierr != nil && ierr != io.EOF {
		send(outFrame{Type: "error", Error: ierr.Error()})
	}
	send(outFrame{Type: "end"})
	close(sendCh)
	<-writerDone
	return nil
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/grpcx/ -run TestStreamUnary -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/grpcx/stream.go internal/grpcx/stream_test.go internal/grpcx/live_test.go
git commit -m "feat(grpcx): live gRPC invoke over WebSocket (unary path)"
```

---

### Task 3: Server-stream, client-stream, and bidi frame sequences

**Files:**
- Modify: `internal/grpcx/stream_test.go`

**Interfaces:**
- Consumes: `Stream`, `streamServer`, `dialWS`, `collect`, `typesOf` from Task 2.

- [ ] **Step 1: Write the failing server-stream test**

Append to `internal/grpcx/stream_test.go`:

```go
func TestStreamServerStreaming(t *testing.T) {
	target := startServer(t, false)
	srv := streamServer(t)
	defer srv.Close()
	c := dialWS(t, srv)
	defer c.Close()

	c.WriteJSON(map[string]any{"type": "init", "request": map[string]any{
		"target": target, "symbol": "shop.v1.UserService/StreamUsers",
	}})
	c.WriteJSON(map[string]any{"type": "send", "data": json.RawMessage(`{}`)})
	c.WriteJSON(map[string]any{"type": "halfClose"})

	frames := collect(t, c)
	msgs := 0
	for _, f := range frames {
		var typ string
		json.Unmarshal(f["type"], &typ)
		if typ == "message" {
			msgs++
		}
	}
	if msgs < 2 {
		t.Errorf("expected >=2 streamed messages, got %d (%v)", msgs, typesOf(frames))
	}
}
```

- [ ] **Step 2: Write the failing client-stream test**

```go
func TestStreamClientStreaming(t *testing.T) {
	target := startServer(t, false)
	srv := streamServer(t)
	defer srv.Close()
	c := dialWS(t, srv)
	defer c.Close()

	c.WriteJSON(map[string]any{"type": "init", "request": map[string]any{
		"target": target, "symbol": "shop.v1.UserService/CountUsers",
	}})
	c.WriteJSON(map[string]any{"type": "send", "data": json.RawMessage(`{"id":1}`)})
	c.WriteJSON(map[string]any{"type": "send", "data": json.RawMessage(`{"id":2}`)})
	c.WriteJSON(map[string]any{"type": "send", "data": json.RawMessage(`{"id":3}`)})
	c.WriteJSON(map[string]any{"type": "halfClose"})

	frames := collect(t, c)
	var msgData string
	for _, f := range frames {
		var typ string
		json.Unmarshal(f["type"], &typ)
		if typ == "message" {
			msgData = string(f["data"])
		}
	}
	if !strings.Contains(msgData, "3") {
		t.Errorf("expected count 3 in response, got %s (%v)", msgData, typesOf(frames))
	}
}
```

- [ ] **Step 3: Write the failing bidi test**

```go
func TestStreamBidi(t *testing.T) {
	target := startServer(t, false)
	srv := streamServer(t)
	defer srv.Close()
	c := dialWS(t, srv)
	defer c.Close()

	c.WriteJSON(map[string]any{"type": "init", "request": map[string]any{
		"target": target, "symbol": "shop.v1.UserService/Chat",
	}})
	c.WriteJSON(map[string]any{"type": "send", "data": json.RawMessage(`{"id":11}`)})
	c.WriteJSON(map[string]any{"type": "send", "data": json.RawMessage(`{"id":22}`)})
	c.WriteJSON(map[string]any{"type": "halfClose"})

	frames := collect(t, c)
	msgs := 0
	for _, f := range frames {
		var typ string
		json.Unmarshal(f["type"], &typ)
		if typ == "message" {
			msgs++
		}
	}
	if msgs != 2 {
		t.Errorf("bidi echo expected 2 messages, got %d (%v)", msgs, typesOf(frames))
	}
}
```

- [ ] **Step 4: Run the three tests**

Run: `go test ./internal/grpcx/ -run 'TestStreamServerStreaming|TestStreamClientStreaming|TestStreamBidi' -v`
Expected: PASS — the Task 2 implementation is kind-agnostic (grpcurl handles each RPC kind); no `stream.go` change should be needed. If bidi hangs, verify the read loop writes each `send` payload immediately and that `halfClose` closes `pw`.

- [ ] **Step 5: Commit**

```bash
git add internal/grpcx/stream_test.go
git commit -m "test(grpcx): cover server-stream, client-stream, and bidi over WebSocket"
```

---

### Task 4: Cancel and error frames

**Files:**
- Modify: `internal/grpcx/stream_test.go`

**Interfaces:**
- Consumes: `Stream`, helpers from Task 2.

- [ ] **Step 1: Write the failing cancel test**

A bidi stream stays open until half-closed; sending `cancel` must terminate it and yield an `end`. Append:

```go
func TestStreamCancel(t *testing.T) {
	target := startServer(t, false)
	srv := streamServer(t)
	defer srv.Close()
	c := dialWS(t, srv)
	defer c.Close()

	c.WriteJSON(map[string]any{"type": "init", "request": map[string]any{
		"target": target, "symbol": "shop.v1.UserService/Chat",
	}})
	c.WriteJSON(map[string]any{"type": "send", "data": json.RawMessage(`{"id":1}`)})
	c.WriteJSON(map[string]any{"type": "cancel"})

	frames := collect(t, c)
	got := typesOf(frames)
	ended := false
	for _, g := range got {
		if g == "end" {
			ended = true
		}
	}
	if !ended {
		t.Errorf("cancel did not produce an end frame; got %v", got)
	}
}
```

- [ ] **Step 2: Write the failing bad-init test**

```go
func TestStreamRejectsMissingTarget(t *testing.T) {
	srv := streamServer(t)
	defer srv.Close()
	c := dialWS(t, srv)
	defer c.Close()

	c.WriteJSON(map[string]any{"type": "init", "request": map[string]any{"symbol": "x/y"}})

	frames := collect(t, c)
	got := typesOf(frames)
	if len(got) == 0 || got[0] != "error" {
		t.Errorf("expected leading error frame for missing target; got %v", got)
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/grpcx/ -run 'TestStreamCancel|TestStreamRejectsMissingTarget' -v`
Expected: PASS (both behaviours are already implemented in Task 2's `Stream`). If `TestStreamCancel` flakes, confirm `cancel()` is called on the `cancel` frame and that `<-invokeDone` returns after context cancellation.

- [ ] **Step 4: Run the whole grpcx package**

Run: `go test ./internal/grpcx/ -v`
Expected: PASS (existing `Invoke` tests plus all new stream tests). No `-race` deadlocks.

- [ ] **Step 5: Commit**

```bash
git add internal/grpcx/stream_test.go
git commit -m "test(grpcx): cover cancel and bad-init frames"
```

---

### Task 5: Wire the `grpc/stream` WebSocket endpoint into the console handler

**Files:**
- Modify: `spector.go` (inside `Handler`, near the `grpc/invoke` branch ~line 862)
- Create: `spector_grpc_stream_test.go`

**Interfaces:**
- Consumes: `grpcx.Stream`, existing `authorized(r, cfg.AccessKey)`, `protoDir` (already computed in `Handler`).
- Produces: an HTTP route: a GET request to a path ending in `grpc/stream` upgrades to a WebSocket and runs one RPC. Unauthorized → 404 before upgrade.

- [ ] **Step 1: Write the failing auth-gate test**

Create `spector_grpc_stream_test.go`:

```go
package spector

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// With an access key configured, an unauthenticated WebSocket upgrade to
// grpc/stream must be refused (the console 404s rather than confirming it is
// there).
func TestGrpcStreamUpgradeRequiresKey(t *testing.T) {
	h := Handler(Config{Dir: "examples/shop", AccessKey: "secret"})
	srv := httptest.NewServer(h)
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/docs/grpc/stream"
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatal("expected upgrade to be refused without the key")
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Errorf("expected 404, got %d", code)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run TestGrpcStreamUpgradeRequiresKey -v`
Expected: FAIL — no `grpc/stream` route yet, so the dial does not 404 as expected (it likely hits the page handler).

- [ ] **Step 3: Add the upgrader and route**

At the top of `spector.go`, add to the import block:

```go
	"github.com/gorilla/websocket"
```

Add a package-level upgrader (near the other `Handler` helpers). `CheckOrigin` enforces same-origin so a foreign page cannot open a console stream:

```go
// grpcStreamUpgrader upgrades console gRPC streaming requests. Same-origin only:
// a cross-origin page must not be able to open a stream against the console.
var grpcStreamUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients (tests, CLIs) send no Origin
		}
		return sameOrigin(origin, r.Host)
	},
}

// sameOrigin reports whether an Origin header matches the request host.
func sameOrigin(origin, host string) bool {
	i := strings.Index(origin, "://")
	if i < 0 {
		return false
	}
	return origin[i+3:] == host
}
```

Inside `Handler`'s returned `http.HandlerFunc`, immediately after the `once.Do(build)` line and **before** the `grpc/invoke` branch, add:

```go
		if strings.HasSuffix(r.URL.Path, "grpc/stream") {
			conn, uerr := grpcStreamUpgrader.Upgrade(w, r, nil)
			if uerr != nil {
				return // Upgrade already wrote an error response
			}
			_ = grpcx.Stream(protoDir, conn)
			return
		}
```

The `authorized` check at the top of the handler already runs before this, so an unauthenticated upgrade is 404'd before it reaches the upgrader.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test . -run TestGrpcStreamUpgradeRequiresKey -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add spector.go spector_grpc_stream_test.go
git commit -m "feat: route grpc/stream to the live gRPC WebSocket handler"
```

---

### Task 6: Console UI — live gRPC session panel

**Files:**
- Modify: `internal/ui/ui.html` (the gRPC method card builder, ~lines 1858–1928)
- Modify: `internal/ui/ui_test.go`

**Interfaces:**
- Consumes: `grpc/stream` WebSocket endpoint (Task 5); existing helpers `wsOrigin()`, `rtLog(box, kind, text)`, `interpolate(v, env)`, `activeEnv()`, `sample(gs, ref)`, `buildGrpcurl(...)`, and the `.log` / `.btnrow` / `.resp` CSS classes already in the page.
- Produces: per-method live session controls with element ids `grpcConnect`, `grpcSend`, `grpcHalfClose`, `grpcCancel` present in the markup for at least one method card, and a `WebSocket` opened to a URL ending in `grpc/stream`.

- [ ] **Step 1: Add the failing UI contract test**

In `internal/ui/ui_test.go`, extend the id list in `TestControlsHaveMarkup` — add these to the `ids` slice:

```go
		"grpcConnect", "grpcSend", "grpcHalfClose", "grpcCancel",
```

Then add a new test pinning the streaming contract:

```go
// The gRPC panel must open a live WebSocket to grpc/stream and speak the frame
// protocol the Go side implements. Renaming either silently breaks streaming.
func TestGrpcStreamContract(t *testing.T) {
	page := string(Page)
	for _, want := range []string{
		`grpc/stream`,
		`"halfClose"`,
		`"cancel"`,
		`"init"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("gRPC streaming page missing %s", want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestControlsHaveMarkup|TestGrpcStreamContract' -v`
Expected: FAIL — the ids and the `grpc/stream` string are not in the page yet.

- [ ] **Step 3: Replace the batch invoke flow with a live session**

In `internal/ui/ui.html`, in the gRPC method card builder, replace the button row + `invokeGrpc` wiring (the block from `const streaming = ...` through the `if (streaming) { ... }` hint, currently ~lines 1879–1889) with the live session below. Keep the request textarea(s) (`msgsWrap` / `.grpc-msg`), the `＋ Add message` button, and the `Copy as grpcurl` button as they are.

```javascript
  // Live streaming session. Every kind uses one WebSocket per invocation; the
  // method kind decides which controls are live (see below).
  const row = document.createElement("div"); row.className = "btnrow";
  const dot = document.createElement("span"); dot.className = "dot"; dot.style.alignSelf = "center";
  const connectBtn = document.createElement("button"); connectBtn.id = "grpcConnect"; connectBtn.className = "primary"; connectBtn.textContent = "Connect";
  const sendBtn = document.createElement("button"); sendBtn.id = "grpcSend"; sendBtn.textContent = "Send"; sendBtn.disabled = true;
  const halfBtn = document.createElement("button"); halfBtn.id = "grpcHalfClose"; halfBtn.textContent = "Half-close"; halfBtn.disabled = true;
  const cancelBtn = document.createElement("button"); cancelBtn.id = "grpcCancel"; cancelBtn.textContent = "Cancel"; cancelBtn.disabled = true;
  const gBtn = document.createElement("button"); gBtn.textContent = "Copy as grpcurl";
  row.appendChild(dot); row.appendChild(connectBtn); row.appendChild(sendBtn); row.appendChild(halfBtn); row.appendChild(cancelBtn); row.appendChild(gBtn);
  body.appendChild(row);

  const gBox = document.createElement("div");
  const log = document.createElement("div"); log.className = "log";
  body.appendChild(gBox); body.appendChild(log);

  gBtn.onclick = () => { const c = buildGrpcurl(svc, m, target.value, reqArea.value); gBox.innerHTML=""; const pre=document.createElement("pre"); pre.textContent=c; gBox.appendChild(pre); navigator.clipboard && navigator.clipboard.writeText(c).then(()=>{ gBtn.textContent="Copied!"; setTimeout(()=>gBtn.textContent="Copy as grpcurl",1200); }); };

  let ws = null;
  const setConnected = (on) => {
    dot.className = "dot " + (on ? "live" : "");
    connectBtn.disabled = on;
    // Client-stream and bidi can send repeatedly; unary/server-stream send once.
    sendBtn.disabled = !on;
    halfBtn.disabled = !(on && m.clientStreaming);
    cancelBtn.disabled = !on;
  };
  const teardown = () => { setConnected(false); dot.className = "dot dead"; ws = null; };

  connectBtn.onclick = () => {
    const env = activeEnv();
    const t = interpolate(target.value || env.vars.grpcHost || "localhost:50051", env);
    log.innerHTML = "";
    ws = new WebSocket(wsOrigin() + location.pathname.replace(/\/$/, "") + "/grpc/stream");
    ws.onopen = () => {
      ws.send(JSON.stringify({ type: "init", request: {
        target: t, symbol: svc.fullName + "/" + m.name,
        tls: env.vars.grpcTLS === "true", insecure: env.vars.grpcInsecure === "true",
        timeoutSec: parseInt(env.vars.grpcTimeoutSec, 10) || 0,
      }}));
    };
    ws.onmessage = (ev) => {
      let f; try { f = JSON.parse(ev.data); } catch (e) { return; }
      if (f.type === "open") { setConnected(true); rtLog(log, "in", "● stream open"); }
      else if (f.type === "message") { rtLog(log, "in", "← " + JSON.stringify(f.data)); }
      else if (f.type === "status") { rtLog(log, f.code === "OK" ? "in" : "err", "status " + f.code + (f.message ? ": " + f.message : "")); }
      else if (f.type === "error") { rtLog(log, "err", "error: " + f.error); }
      else if (f.type === "end") { rtLog(log, "in", "● stream end"); }
    };
    ws.onclose = teardown;
    ws.onerror = () => rtLog(log, "err", "socket error");
  };

  const sendCurrent = () => {
    if (!ws || ws.readyState !== 1) return;
    const env = activeEnv();
    const msgs = msgsWrap.querySelectorAll(".grpc-msg");
    for (const t of msgs) {
      let data; try { data = JSON.parse(interpolate(t.value, env)); } catch (e) { rtLog(log, "err", "invalid JSON: " + e); return; }
      ws.send(JSON.stringify({ type: "send", data }));
      rtLog(log, "out", "→ " + JSON.stringify(data));
    }
    // Unary and server-stream only ever send once, so half-close immediately.
    if (!m.clientStreaming) { ws.send(JSON.stringify({ type: "halfClose" })); sendBtn.disabled = true; }
  };
  sendBtn.onclick = sendCurrent;
  halfBtn.onclick = () => { if (ws && ws.readyState === 1) { ws.send(JSON.stringify({ type: "halfClose" })); halfBtn.disabled = true; sendBtn.disabled = true; rtLog(log, "out", "◁ half-close"); } };
  cancelBtn.onclick = () => { if (ws && ws.readyState === 1) ws.send(JSON.stringify({ type: "cancel" })); };
```

Notes for the implementer:
- `wsOrigin()` and `rtLog(box, kind, text)` already exist (search the file). `rtLog` pretty-prints JSON strings; passing an already-stringified object is fine.
- The endpoint path mirrors how the page fetches `grpc.json` relative to the console mount: it appends `/grpc/stream` to the current `location.pathname`. Verify against how `fetch("grpc.json")` resolves in the page; if the page fetches with a relative path, use the same base.
- Remove the now-unused `invBtn`/`out`/old `invokeGrpc` call for this panel. Leave the standalone `invokeGrpc` function and the "run all" runner (which posts to `grpc/invoke`) untouched.

- [ ] **Step 4: Run the UI tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS (`TestControlsHaveMarkup`, `TestGrpcStreamContract`, and the existing embed/format/endpoint tests).

- [ ] **Step 5: Manual smoke check (optional but recommended)**

Run the shop example's gRPC server, then serve the console against it and open a bidi method:

```bash
go run ./examples/shop &   # starts the shop gRPC server
# In another shell, serve the console for the shop source and open it in a browser,
# switch to the gRPC tab, pick Chat, Connect, Send a couple messages, Half-close.
```

Expected: response messages appear in the log incrementally; Half-close ends the stream with an OK status.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/ui.html internal/ui/ui_test.go
git commit -m "feat(ui): interactive gRPC streaming session in the console"
```

---

### Task 7: Full suite and README

**Files:**
- Modify: `README.md` (the gRPC console section, if one exists — otherwise the console/features section)

**Interfaces:** none.

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 2: Document the feature**

Find the console/gRPC section in `README.md` (`grep -n -i "grpc\|console" README.md`) and add a short paragraph noting that the console now invokes gRPC methods interactively over a WebSocket: unary and server-streaming send once and stream responses back live; client-streaming and bidi support sending multiple messages with an explicit Half-close, plus Cancel. Match the surrounding heading style and terseness.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: interactive gRPC streaming in the console"
```

---

## Self-Review

**Spec coverage:**
- WebSocket transport, one RPC per connection → Tasks 2, 5. ✓
- Pipe-backed request supplier + halfClose → Task 2. ✓
- Custom handler pushing message/status/error frames → Task 2. ✓
- Writer goroutine for concurrent-write safety → Task 2. ✓
- All four method kinds interactive → Tasks 2 (unary), 3 (server/client/bidi). ✓
- Frame protocol (init/send/halfClose/cancel; open/message/status/error/end) → Task 2, exercised in 3–4, consumed by UI in 6. ✓
- cancel, timeout, error frames, bad init → Task 4. ✓
- Auth gate before upgrade + same-origin CheckOrigin → Task 5. ✓
- UI live session (Connect/Send/Half-close/Cancel + incremental log) → Task 6. ✓
- `grpc/invoke` and run-all runner untouched → stated in Global Constraints; Task 6 note preserves them. ✓
- Tests: grpcx streaming, ui contract, handler auth → Tasks 2–6. ✓

**Placeholder scan:** No TBD/TODO; every code step carries real code. Task 3/4 "no impl change expected" steps still run concrete tests and give a concrete fallback if they fail.

**Type consistency:** `Stream(protoDir string, conn *websocket.Conn) error`, `inFrame`/`outFrame`, and `wsHandler` method set match grpcurl's `InvocationEventHandler` (`OnResolveMethod`, `OnSendHeaders`, `OnReceiveHeaders`, `OnReceiveResponse`, `OnReceiveTrailers`). Frame `type` string constants are identical across backend (Task 2), tests (Tasks 2–4), and UI (Task 6). `req.TimeoutSec`/`dialCreds`/`descriptorSource`/`timeoutOf` reuse the existing `invoke.go` helpers verbatim.
