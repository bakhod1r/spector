package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The SSE stream never ends on its own, so the test cancels the request once
// it has seen enough — the same thing a browser does when you navigate away.
func TestSSEStreamsEvents(t *testing.T) {
	srv := httptest.NewServer(router())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}

	buf := make([]byte, 4096)
	n, err := res.Body.Read(buf)
	if err != nil && n == 0 {
		t.Fatalf("read: %v", err)
	}
	got := string(buf[:n])
	if !strings.Contains(got, "event:tick") {
		t.Errorf("stream does not carry the named event:\n%s", got)
	}
	if !strings.Contains(got, `"seq":1`) {
		t.Errorf("stream does not carry a sequence number:\n%s", got)
	}
}

// The event name is selectable so a tester can exercise the pane's named-event
// subscription with something other than the default.
func TestSSEEventNameIsConfigurable(t *testing.T) {
	srv := httptest.NewServer(router())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events?event=custom", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	buf := make([]byte, 2048)
	n, _ := res.Body.Read(buf)
	if got := string(buf[:n]); !strings.Contains(got, "event:custom") {
		t.Errorf("stream does not use the requested name:\n%s", got)
	}
}

// Cancelling the request must end the handler rather than leave it writing to
// a dead socket.
func TestSSEStopsWhenClientDisconnects(t *testing.T) {
	srv := httptest.NewServer(router())
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 1024)
	if _, err := res.Body.Read(buf); err != nil {
		t.Fatalf("first read: %v", err)
	}
	cancel()
	res.Body.Close()

	// srv.Close blocks until outstanding handlers return, so reaching the end
	// of this test at all is the assertion.
	done := make(chan struct{})
	go func() { srv.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not stop after the client disconnected")
	}
}

func wsDial(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func TestWebSocketPushesMessages(t *testing.T) {
	srv := httptest.NewServer(router())
	defer srv.Close()

	conn := wsDial(t, srv)
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(msg), `"seq":1`) {
		t.Errorf("first message = %s, want a sequence number", msg)
	}

	// Several messages arrive without the client asking for them.
	_, msg2, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if !strings.Contains(string(msg2), `"seq":2`) {
		t.Errorf("second message = %s", msg2)
	}
}

func TestWebSocketEchoesInput(t *testing.T) {
	srv := httptest.NewServer(router())
	defer srv.Close()

	conn := wsDial(t, srv)
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}

	// The push loop is also running, so read until the echo shows up.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for i := 0; i < 10; i++ {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.Contains(string(msg), "echo: ping") {
			return
		}
	}
	t.Error("never received the echo")
}

// Closing the connection must end the handler's push loop.
func TestWebSocketStopsWhenClientCloses(t *testing.T) {
	srv := httptest.NewServer(router())

	conn := wsDial(t, srv)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read: %v", err)
	}
	conn.Close()

	done := make(chan struct{})
	go func() { srv.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not stop after the client closed")
	}
}

// A plain GET without the upgrade headers is rejected by the upgrader rather
// than hanging or panicking.
func TestWebSocketRejectsNonUpgrade(t *testing.T) {
	w := do(t, router(), http.MethodGet, "/ws", "")
	if w.Code == http.StatusOK {
		t.Errorf("status = %d, want an error for a non-upgrade request", w.Code)
	}
}
