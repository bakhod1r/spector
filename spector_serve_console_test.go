package spector

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ServeConsole runs the console on a real address. The test binds an ephemeral
// port, serves in a goroutine, and confirms the console page is reachable —
// the property that makes it a usable long-lived process rather than the
// throwaway runner it replaces.
func TestServeConsoleServesPage(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // ServeConsole opens its own listener on this address

	go func() { _ = ServeConsole(addr, Config{Dir: "examples/shop", Title: "Shop", Version: "1.0.0"}) }()

	base := "http://" + addr
	deadline := time.Now().Add(3 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get(base + "/docs/")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("console never came up on %s: %v", addr, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /docs/ = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}
