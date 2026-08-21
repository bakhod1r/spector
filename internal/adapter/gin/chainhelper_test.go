package gin

import (
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// A project's own chain helper — chain(guard, h) rather than append(guards, h)
// — is a call like any other, so the name behind the registration resolved to
// the helper's declaration. Nothing downstream noticed it was the wrong
// function: every route registered through it was documented with the helper's
// doc comment as its summary ("builds one route's handler slice…", repeated on
// every endpoint) and with none of the handler's own types.
func TestHandlerBehindAProjectChainHelper(t *testing.T) {
	routes, _, _, err := (&Adapter{}).Scan("testdata/chainhelper")
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]core.Route{}
	for _, r := range routes {
		byPath[r.Path] = r
	}

	for _, tc := range []struct {
		path, handler, summary, request, response string
	}{
		{"/signup", "createSignup", "registers a new account.", "SignupRequest", "SignupResponse"},
		{"/signup/verify/request", "requestVerification", "sends a verification code.", "VerifyRequest", "VerifyResponse"},
	} {
		r, ok := byPath[tc.path]
		if !ok {
			t.Errorf("route %s not found in %v", tc.path, byPath)
			continue
		}
		if r.HandlerName != tc.handler {
			t.Errorf("%s: handler = %q, want %q", tc.path, r.HandlerName, tc.handler)
		}
		if r.Summary != tc.summary {
			t.Errorf("%s: summary = %q, want %q", tc.path, r.Summary, tc.summary)
		}
		if r.RequestType != tc.request {
			t.Errorf("%s: request = %q, want %q", tc.path, r.RequestType, tc.request)
		}
		found := false
		for _, resp := range r.Responses {
			if resp.Type == tc.response {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no %s response; got %v", tc.path, tc.response, r.Responses)
		}
	}
}
