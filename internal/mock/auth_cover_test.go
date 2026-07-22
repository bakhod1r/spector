package mock

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

func authDoc(schemes map[string]*core.SecurityScheme, opSec, docSec []core.SecurityRequirement) *core.Document {
	op := core.NewOperation("x")
	op.SetResponse(200, &core.Response{Description: "ok"})
	op.Security = opSec
	d := docWith(nil, "/locked", "get", op)
	d.Components.SecuritySchemes = schemes
	d.Security = docSec
	return d
}

func authGet(t *testing.T, doc *core.Document, mod func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/locked", nil)
	if mod != nil {
		mod(r)
	}
	HandlerWith(doc, Options{EnforceAuth: true}).ServeHTTP(w, r)
	return w
}

var bearer = map[string]*core.SecurityScheme{
	"bearerAuth": {Type: "http", Scheme: "bearer"},
}

func TestEnforcedAuthRejectsMissingBearer(t *testing.T) {
	doc := authDoc(bearer, []core.SecurityRequirement{{"bearerAuth": {}}}, nil)
	w := authGet(t, doc, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("= %d, want 401", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q", got)
	}
	if !strings.Contains(w.Body.String(), "bearerAuth") {
		t.Errorf("detail does not name the scheme:\n%s", w.Body.String())
	}
}

func TestEnforcedAuthAcceptsAnyBearerToken(t *testing.T) {
	doc := authDoc(bearer, []core.SecurityRequirement{{"bearerAuth": {}}}, nil)
	w := authGet(t, doc, func(r *http.Request) { r.Header.Set("Authorization", "Bearer x") })
	if w.Code != http.StatusOK {
		t.Errorf("= %d, want 200", w.Code)
	}
}

// The operation falls back to the document's global security.
func TestEnforcedAuthUsesGlobalSecurity(t *testing.T) {
	doc := authDoc(bearer, nil, []core.SecurityRequirement{{"bearerAuth": {}}})
	if w := authGet(t, doc, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("= %d, want 401 via global security", w.Code)
	}
}

func TestEnforcedAuthUndocumentedOperationIsOpen(t *testing.T) {
	doc := authDoc(bearer, nil, nil)
	if w := authGet(t, doc, nil); w.Code != http.StatusOK {
		t.Errorf("= %d, want 200 for an operation with no requirements", w.Code)
	}
}

// Requirements are alternatives: satisfying the second one is enough.
func TestEnforcedAuthAlternatives(t *testing.T) {
	schemes := map[string]*core.SecurityScheme{
		"bearerAuth": {Type: "http", Scheme: "bearer"},
		"keyAuth":    {Type: "apiKey", In: "header", Name: "X-Key"},
	}
	reqs := []core.SecurityRequirement{{"bearerAuth": {}}, {"keyAuth": {}}}
	doc := authDoc(schemes, reqs, nil)
	if w := authGet(t, doc, func(r *http.Request) { r.Header.Set("X-Key", "k") }); w.Code != http.StatusOK {
		t.Errorf("= %d, want 200 via the second alternative", w.Code)
	}
}

// A requirement naming an undefined scheme cannot be satisfied.
func TestEnforcedAuthUndefinedSchemeFails(t *testing.T) {
	doc := authDoc(nil, []core.SecurityRequirement{{"ghost": {}}}, nil)
	w := authGet(t, doc, func(r *http.Request) { r.Header.Set("Authorization", "Bearer x") })
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "ghost") {
		t.Errorf("= %d %s, want 401 naming ghost", w.Code, w.Body.String())
	}
}

// An empty requirement object can satisfy nothing.
func TestEnforcedAuthEmptyRequirement(t *testing.T) {
	doc := authDoc(bearer, []core.SecurityRequirement{{}}, nil)
	if w := authGet(t, doc, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("= %d, want 401 for an empty requirement", w.Code)
	}
}

func TestSchemeSatisfiedShapes(t *testing.T) {
	req := func(mod func(*http.Request)) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/locked?key=q", nil)
		if mod != nil {
			mod(r)
		}
		return r
	}
	cases := []struct {
		name   string
		scheme *core.SecurityScheme
		mod    func(*http.Request)
		want   bool
	}{
		{"basic ok", &core.SecurityScheme{Type: "http", Scheme: "basic"},
			func(r *http.Request) { r.Header.Set("Authorization", "Basic dXNlcg==") }, true},
		{"basic empty", &core.SecurityScheme{Type: "http", Scheme: "basic"}, nil, false},
		{"bearer bare word", &core.SecurityScheme{Type: "http", Scheme: "bearer"},
			func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") }, false},
		{"apiKey query", &core.SecurityScheme{Type: "apiKey", In: "query", Name: "key"}, nil, true},
		{"apiKey cookie ok", &core.SecurityScheme{Type: "apiKey", In: "cookie", Name: "sid"},
			func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "sid", Value: "s"}) }, true},
		{"apiKey cookie missing", &core.SecurityScheme{Type: "apiKey", In: "cookie", Name: "sid"}, nil, false},
		{"apiKey header missing", &core.SecurityScheme{Type: "apiKey", In: "header", Name: "X-Key"}, nil, false},
		{"unknown type falls back to Authorization", &core.SecurityScheme{Type: "oauth2"},
			func(r *http.Request) { r.Header.Set("Authorization", "anything") }, true},
		{"unknown type without header", &core.SecurityScheme{Type: "oauth2"}, nil, false},
	}
	for _, tc := range cases {
		if got := schemeSatisfied(tc.scheme, req(tc.mod)); got != tc.want {
			t.Errorf("%s: = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDescribeScheme(t *testing.T) {
	cases := []struct {
		scheme *core.SecurityScheme
		want   string
	}{
		{&core.SecurityScheme{Type: "http", Scheme: "basic"}, "s (Authorization: Basic ...)"},
		{&core.SecurityScheme{Type: "http", Scheme: "bearer"}, "s (Authorization: Bearer ...)"},
		{&core.SecurityScheme{Type: "apiKey", Name: "X-Key", In: "query"}, "s (X-Key in query)"},
		{&core.SecurityScheme{Type: "apiKey", Name: "X-Key"}, "s (X-Key in header)"},
		{&core.SecurityScheme{Type: "oauth2"}, "s"},
	}
	for _, tc := range cases {
		if got := describeScheme("s", tc.scheme); got != tc.want {
			t.Errorf("= %q, want %q", got, tc.want)
		}
	}
}

// A declared example beats a fabricated value.
func TestSampleUsesDeclaredExample(t *testing.T) {
	got := Sample(core.NewDocument("t", "1"), &core.Schema{Type: "string", Example: "from-the-doc"}, nil)
	if got != "from-the-doc" {
		t.Errorf("= %v, want the declared example", got)
	}
}
