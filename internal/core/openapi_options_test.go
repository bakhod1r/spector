package core

import "testing"

// TestOperationOptions exercises each functional option through NewOperation,
// which is how callers apply them.
func TestOperationOptions(t *testing.T) {
	src := &Source{File: "a.go", Line: 3}
	op := NewOperation("op",
		WithSource(src),
		WithCalls([]Call{{Kind: "http"}}),
		WithTags([]string{"users"}),
		WithDeprecated(true),
		WithMiddleware([]Middleware{{Name: "Auth"}}),
		WithSecurity([]SecurityRequirement{{"bearer": {}}}),
		WithRealtime("websocket"),
	)
	if op.Source != src {
		t.Errorf("Source = %+v, want %+v", op.Source, src)
	}
	if len(op.Calls) != 1 || op.Calls[0].Kind != "http" {
		t.Errorf("Calls = %+v", op.Calls)
	}
	if len(op.Tags) != 1 || op.Tags[0] != "users" {
		t.Errorf("Tags = %+v", op.Tags)
	}
	if !op.Deprecated {
		t.Error("Deprecated not set")
	}
	if len(op.Middleware) != 1 || op.Middleware[0].Name != "Auth" {
		t.Errorf("Middleware = %+v", op.Middleware)
	}
	if len(op.Security) != 1 {
		t.Errorf("Security = %+v", op.Security)
	}
	if op.Realtime != "websocket" {
		t.Errorf("Realtime = %q", op.Realtime)
	}
}
