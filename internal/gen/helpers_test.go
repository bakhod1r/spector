package gen

import "testing"

func TestParameterKey(t *testing.T) {
	cases := map[string]string{
		"X-Request-ID": "XRequestID",
		"Authorization": "Authorization",
		"---":          "Header",
		"":             "Header",
	}
	for in, want := range cases {
		if got := parameterKey(in); got != want {
			t.Errorf("parameterKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMiddlewareStatusText(t *testing.T) {
	if got := middlewareStatusText(401, "Auth"); got != "Unauthorized (from Auth)" {
		t.Errorf("got %q", got)
	}
	if got := middlewareStatusText(488, "Auth"); got != "Rejected by Auth" {
		t.Errorf("got %q", got)
	}
}
