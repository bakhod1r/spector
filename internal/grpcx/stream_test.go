//go:build grpclive

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
