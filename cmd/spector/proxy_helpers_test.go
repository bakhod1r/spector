package main

import (
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// freePort returns a port nothing is listening on, so a proxy test does not
// collide with another test or a real service.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// proxyClient is pinned to a single connection per host on purpose. The proxy
// inspects an exchange *after* the client already has its response, so a test
// that stops the proxy as soon as its last Get returns can beat the inspection
// to the report. With one connection, the transport cannot start the next
// request until the previous handler has returned — inspection included — so
// a follow-up request through this client is an exact barrier rather than a
// guessed sleep.
var proxyClient = &http.Client{
	Transport: &http.Transport{MaxConnsPerHost: 1, DisableKeepAlives: false},
	Timeout:   10 * time.Second,
}

// sendGet and sendPost drive traffic at the proxy and then drain and close the
// body. Draining is not politeness here: proxyClient holds a single
// connection, and a body left open never returns it to the pool, so the next
// request through the client — the settle barrier — would block until its
// timeout.
func sendGet(t *testing.T, url string) {
	t.Helper()
	res, err := proxyClient.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
}

func sendPost(t *testing.T, url, contentType, body string) {
	t.Helper()
	res, err := proxyClient.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
}

// settle blocks until the proxy has finished observing every exchange sent
// through proxyClient before it. See proxyClient for why one more request is
// enough.
func settle(t *testing.T, base string) {
	t.Helper()
	res, err := proxyClient.Get(base + "/__settle")
	if err != nil {
		t.Fatalf("settling the proxy: %v", err)
	}
	res.Body.Close()
}

// waitForListener blocks until the proxy is accepting connections, so traffic
// is not sent before it is up.
func waitForListener(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if res, err := http.Get(base + "/__ping"); err == nil {
			res.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the proxy never started listening")
}

// interruptSelf sends the process the signal a person's Ctrl-C would, which is
// how the proxy is meant to stop and the path that writes its reports.
func interruptSelf(t *testing.T) {
	t.Helper()
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
}

// safeWriter guards a strings.Builder written from the proxy goroutine while
// the test goroutine reads it, so the race detector stays quiet.
type safeWriter struct {
	mu sync.Mutex
	w  interface{ WriteString(string) (int, error) }
}

func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.WriteString(string(p))
}
