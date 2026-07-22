package realtime

import "testing"

// The stream setup lives in a helper reached through a method call; following
// it finds SSE, the weaker signal, without a websocket to override it.
func TestDetectsDelegatedSSEThroughAMethod(t *testing.T) {
	if got := detectIn(t, "delegatingSSE"); got != SSE {
		t.Errorf("= %q, want sse", got)
	}
}

// Header().Set with variables carries no literal to read; not a signal.
func TestHeaderSetWithVariablesIsNotSSE(t *testing.T) {
	if got := detectIn(t, "headerFromVars"); got != "" {
		t.Errorf("= %q, want nothing", got)
	}
}
