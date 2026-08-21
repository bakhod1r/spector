//go:build !grpclive

package grpcx

import (
	"strings"
	"testing"
)

// A build without live gRPC must say which build it is, so the console's error
// tells a reader what to do rather than looking like a failed call.
func TestDisabledExplainsItself(t *testing.T) {
	if _, err := Invoke("proto", Request{Target: "127.0.0.1:1"}); err == nil ||
		!strings.Contains(err.Error(), "grpclive") {
		t.Errorf("Invoke error = %v, want the rebuild instruction", err)
	}
	if err := Stream("proto", nil); err == nil || !strings.Contains(err.Error(), "grpclive") {
		t.Errorf("Stream error = %v, want the rebuild instruction", err)
	}
}
