package middleware_test

import (
	"testing"

	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/middleware"
)

// A project's own middleware is the case that matters: real codebases are full
// of guards called SignMiddleware, TenantGuard, PlatformCheck — names that mean
// everything to the team and nothing to a pattern list. Reading the body is
// what makes them documentable.
func signed(t *testing.T) core.Middleware {
	t.Helper()
	r := route(t, "GET /v1/orders")
	for _, m := range r.Middleware {
		if m.Name == "sign.Handler" {
			return m
		}
	}
	t.Fatalf("the custom middleware was not attributed to the route: %v", names(r))
	return core.Middleware{}
}

// Every header the guard reads is a header every guarded request must send.
// This is what a client author needs and what no name-based rule could give.
func TestCustomMiddlewareRevealsItsRequiredHeaders(t *testing.T) {
	got := signed(t)
	want := []string{"X-Platform", "X-Request-ID", "X-Sign", "X-Time-Unix"}
	if len(got.Headers) != len(want) {
		t.Fatalf("headers = %v, want %v", got.Headers, want)
	}
	for i := range want {
		if got.Headers[i] != want[i] {
			t.Errorf("headers = %v, want %v", got.Headers, want)
			break
		}
	}
}

// The statuses it aborts with are statuses every guarded endpoint can return,
// including non-standard ones a client would otherwise never expect.
func TestCustomMiddlewareRevealsItsRejectionStatuses(t *testing.T) {
	got := signed(t)
	want := map[int]bool{488: false, 498: false, 409: false, 500: false}
	for _, s := range got.Statuses {
		if _, expected := want[s]; !expected {
			t.Errorf("unexpected status %d in %v", s, got.Statuses)
			continue
		}
		want[s] = true
	}
	for code, found := range want {
		if !found {
			t.Errorf("status %d is missing from %v", code, got.Statuses)
		}
	}
}

// Its name matches nothing, but a middleware demanding an X-Sign header is a
// credential check whatever it is called.
func TestCustomMiddlewareIsRecognisedAsAuthFromItsBody(t *testing.T) {
	got := signed(t)
	if got.Kind != middleware.KindAuth {
		t.Errorf("kind = %q, want auth — it requires a signing header", got.Kind)
	}
	if got.Scheme == "" {
		t.Error("no security scheme implied by a middleware that rejects unsigned requests")
	}
}

// Reading the body must not invent constraints where there are none.
func TestOrdinaryMiddlewareReportsNoHeadersOrStatuses(t *testing.T) {
	r := route(t, "GET /health")
	for _, m := range r.Middleware {
		if len(m.Headers) > 0 {
			t.Errorf("%s reports headers %v", m.Name, m.Headers)
		}
		if len(m.Statuses) > 0 {
			t.Errorf("%s reports statuses %v", m.Name, m.Statuses)
		}
	}
}

// Status codes written as http constants are as common as literals.
func TestHTTPStatusConstantsAreResolved(t *testing.T) {
	got := signed(t)
	for _, s := range got.Statuses {
		if s == 500 {
			return
		}
	}
	t.Errorf("http.StatusInternalServerError was not resolved: %v", got.Statuses)
}
