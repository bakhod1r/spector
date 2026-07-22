package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// gorilla/websocket: the most common spelling.
func gorillaWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
}

// A wrapper type around the upgrader must still be recognised: the method name
// is the signal, not the receiver's type.
func wrappedWS(c *gin.Context) {
	conn, err := myUpgrader.Upgrade(c.Writer, c.Request, nil)
	_, _ = conn, err
}

// nhooyr/coder: websocket.Accept.
func acceptWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	_, _ = conn, err
}

// gobwas/ws.
func gobwasWS(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	_, _ = conn, err
}

// SSE by content type.
func sseByHeader(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Write([]byte("data: hi\n\n"))
}

// SSE via gin's helper.
func sseByHelper(c *gin.Context) {
	c.SSEvent("tick", gin.H{"n": 1})
}

// The upgrade lives in a helper one level down.
func delegatingWS(c *gin.Context) {
	serveSocket(c)
}

func serveSocket(c *gin.Context) {
	upgrader.Upgrade(c.Writer, c.Request, nil)
}

// Ordinary handlers, including ones with tempting names and methods.
func listUsers(c *gin.Context) {
	c.JSON(200, []string{})
}

// A net.Listener's Accept takes no arguments and must not be mistaken for a
// WebSocket accept.
func notASocket(c *gin.Context) {
	conn, err := listener.Accept()
	_, _ = conn, err
}

// An unrelated method that happens to be called Upgrade with one argument.
func notAnUpgrade(c *gin.Context) {
	db.Upgrade("schema")
}

// A handler that mentions the content type in a comment only: text/event-stream
func mentionsOnly(c *gin.Context) {
	c.JSON(200, "text/event-stream")
}
