package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Endpoints for the console's Realtime tab. They emit a small, finite stream
// so a tester sees messages arrive and the connection close cleanly rather
// than watching an idle socket.

const (
	realtimeInterval = 500 * time.Millisecond
)

// sseHandler streams text/event-stream. EventSource is GET-only and cannot
// carry custom headers, so anything auth-like has to ride in the query string.
func sseHandler(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming unsupported")
		return
	}

	name := c.DefaultQuery("event", "tick")
	// Stream until the client goes away. A stream that ends on its own makes
	// EventSource reconnect on a timer, which looks like a broken endpoint in
	// the console rather than a finished one.
	for seq := 1; ; seq++ {
		select {
		case <-c.Request.Context().Done():
			return // client navigated away; stop rather than write to a dead socket
		default:
		}
		c.SSEvent(name, gin.H{"seq": seq, "at": time.Now().UTC().Format(time.RFC3339Nano)})
		flusher.Flush()
		time.Sleep(realtimeInterval)
	}
}

// The console is served from the same origin, but a tester may open it from
// elsewhere, so the upgrade is not origin-restricted in this example.
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsHandler echoes whatever it receives and, absent input, pushes a few
// messages so the pane shows traffic immediately.
func wsHandler(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // Upgrade already wrote the error response
	}
	defer conn.Close()

	// gorilla/websocket allows at most one concurrent writer, so the reader
	// hands echoes to the write loop instead of writing them itself.
	type outbound struct {
		kind int
		data []byte
	}
	echoes := make(chan outbound, 8)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			select {
			case echoes <- outbound{kind: mt, data: append([]byte("echo: "), msg...)}:
			case <-done:
				return
			}
		}
	}()

	tick := time.NewTicker(realtimeInterval)
	defer tick.Stop()

	for seq := 1; ; seq++ {
		msg := fmt.Sprintf(`{"seq":%d,"at":%q}`, seq, time.Now().UTC().Format(time.RFC3339Nano))
		if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			return
		}
		// Drain any echoes that arrived, then wait for the next tick.
		for draining := true; draining; {
			select {
			case out := <-echoes:
				if err := conn.WriteMessage(out.kind, out.data); err != nil {
					return
				}
			case <-done: // reader ended: the client closed or errored
				return
			case <-tick.C:
				draining = false
			}
		}
	}
}
