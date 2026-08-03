package main

import (
	"net"
	"net/http"
	"os"
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
