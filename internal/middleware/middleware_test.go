package middleware_test

import (
	"strings"
	"testing"

	ginadapter "github.com/user/specter/internal/adapter/gin"
	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/middleware"
)

// scan runs the real gin adapter over the fixture, so these tests exercise the
// whole path from source text to route rather than the index in isolation.
func scan(t *testing.T) map[string]core.Route {
	t.Helper()
	routes, _, err := (&ginadapter.Adapter{}).Scan("testdata/app")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]core.Route{}
	for _, r := range routes {
		byKey[strings.ToUpper(r.Method)+" "+r.Path] = r
	}
	return byKey
}

func route(t *testing.T, key string) core.Route {
	t.Helper()
	r, ok := scan(t)[key]
	if !ok {
		t.Fatalf("no route %q", key)
	}
	return r
}

func names(r core.Route) []string {
	out := make([]string, 0, len(r.Middleware))
	for _, m := range r.Middleware {
		out = append(out, m.Name)
	}
	return out
}

func has(r core.Route, name string) bool {
	for _, m := range r.Middleware {
		if m.Name == name {
			return true
		}
	}
	return false
}

// ---- ordering, which is the whole difficulty ----

// r.Use(x) applies to what is registered after it. A generator that ignored
// ordering would mark these public routes as authenticated, and the document
// would look entirely plausible while being wrong.
func TestRoutesRegisteredBeforeUseAreNotAffected(t *testing.T) {
	for _, key := range []string{"GET /health", "POST /login"} {
		r := route(t, key)
		if has(r, "AuthRequired") {
			t.Errorf("%s: %v — AuthRequired is registered after this route", key, names(r))
		}
		if !has(r, "Logger") {
			t.Errorf("%s: %v — Logger is registered before it and should apply", key, names(r))
		}
	}
}

func TestRoutesRegisteredAfterUseAreAffected(t *testing.T) {
	if r := route(t, "GET /profile"); !has(r, "AuthRequired") {
		t.Errorf("= %v, want AuthRequired", names(r))
	}
}

// A group captures what was in effect when it was created, not when its routes
// are registered. Its routes appear further down the file, so using their
// position would wrongly pick up middleware added in between.
func TestGroupInheritsFromItsCreationPoint(t *testing.T) {
	r := route(t, "GET /public/docs")
	if has(r, "AuthRequired") {
		t.Errorf("= %v — the group was created before AuthRequired was added", names(r))
	}
	if !has(r, "Logger") {
		t.Errorf("= %v, want the middleware that was in effect", names(r))
	}
}

func TestGroupAddsItsOwnMiddleware(t *testing.T) {
	r := route(t, "GET /admin/users")
	for _, want := range []string{"Logger", "AuthRequired", "RequireAPIKey"} {
		if !has(r, want) {
			t.Errorf("= %v, want %s", names(r), want)
		}
	}
}

func TestNestedGroupInheritsEveryLevel(t *testing.T) {
	r := route(t, "DELETE /admin/super/users/{id}")
	for _, want := range []string{"Logger", "AuthRequired", "RequireAPIKey"} {
		if !has(r, want) {
			t.Errorf("= %v, want %s inherited through the nested group", names(r), want)
		}
	}
}

// Middleware passed on the registration itself applies to that route alone.
func TestRouteLevelMiddleware(t *testing.T) {
	if r := route(t, "POST /upload"); !has(r, "RateLimit") {
		t.Errorf("= %v, want RateLimit", names(r))
	}
	if r := route(t, "GET /profile"); has(r, "RateLimit") {
		t.Errorf("= %v — RateLimit belongs to /upload alone", names(r))
	}
}

// The last argument is the handler, never middleware.
func TestHandlerIsNotReportedAsMiddleware(t *testing.T) {
	if r := route(t, "POST /upload"); has(r, "upload") {
		t.Errorf("= %v — the handler was counted as middleware", names(r))
	}
}

// ---- classification by name ----

func TestAuthMiddlewareImpliesASecurityScheme(t *testing.T) {
	r := route(t, "GET /profile")
	for _, m := range r.Middleware {
		if m.Name == "AuthRequired" && m.Scheme == "" {
			t.Errorf("AuthRequired implied no security scheme: %+v", m)
		}
	}
}

// Knowing that a logger runs is context; claiming it protects the endpoint
// would be a false statement about the API's contract.
func TestNonAuthMiddlewareImpliesNoScheme(t *testing.T) {
	for _, m := range route(t, "GET /health").Middleware {
		if m.Scheme != "" {
			t.Errorf("%s implied scheme %q", m.Name, m.Scheme)
		}
	}
}

func TestClassificationOfCommonNames(t *testing.T) {
	cases := map[string]struct{ kind, scheme string }{
		"JWTMiddleware":    {middleware.KindAuth, "bearerAuth"},
		"jwt.Auth":         {middleware.KindAuth, "bearerAuth"},
		"RequireBearer":    {middleware.KindAuth, "bearerAuth"},
		"BasicAuth":        {middleware.KindAuth, "basicAuth"},
		"RequireAPIKey":    {middleware.KindAuth, "apiKeyAuth"},
		"api_key_required": {middleware.KindAuth, "apiKeyAuth"},
		"SessionAuth":      {middleware.KindAuth, "sessionAuth"},
		"CORSMiddleware":   {middleware.KindCORS, ""},
		"RateLimit":        {middleware.KindRateLimit, ""},
		"Throttle":         {middleware.KindRateLimit, ""},
		"Logger":           {middleware.KindLogging, ""},
		"Recovery":         {middleware.KindRecovery, ""},
		"Gzip":             {middleware.KindCompress, ""},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got := middleware.Classify(name)
			if got.Kind != want.kind {
				t.Errorf("kind = %q, want %q", got.Kind, want.kind)
			}
			if got.Scheme != want.scheme {
				t.Errorf("scheme = %q, want %q", got.Scheme, want.scheme)
			}
		})
	}
}

// An unrecognised middleware is still reported. Its presence is real even when
// its purpose is unknown, and dropping it would hide that something runs there.
func TestUnknownMiddlewareIsReportedWithoutAKind(t *testing.T) {
	got := middleware.Classify("RequestID")
	if got.Name != "RequestID" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Kind != "" || got.Scheme != "" {
		t.Errorf("invented kind %q / scheme %q for an unknown name", got.Kind, got.Scheme)
	}
}

// "oauth" contains "auth", and "basicauth" contains both. The more specific
// pattern has to win or every scheme collapses into bearerAuth.
func TestSpecificNamesBeatGenericOnes(t *testing.T) {
	if got := middleware.Classify("OAuth2Middleware"); got.Scheme != "oauth2" {
		t.Errorf("OAuth2 classified as %q, want oauth2", got.Scheme)
	}
	if got := middleware.Classify("BasicAuthMiddleware"); got.Scheme != "basicAuth" {
		t.Errorf("BasicAuth classified as %q, want basicAuth", got.Scheme)
	}
}

// ---- what reaches the document ----

func TestSecurityRequirementsReachTheOperation(t *testing.T) {
	r := route(t, "GET /admin/users")
	if schemes := middleware.Chain(r.Middleware).Schemes(); len(schemes) < 2 {
		t.Errorf("schemes = %v, want both the bearer and the API key", schemes)
	}
}

func TestSchemeDefinitionsAreUsable(t *testing.T) {
	for _, name := range []string{"bearerAuth", "basicAuth", "apiKeyAuth", "sessionAuth", "oauth2"} {
		t.Run(name, func(t *testing.T) {
			def := middleware.SchemeDefinition(name)
			if def.Type == "" {
				t.Error("no type; the scheme would not validate")
			}
			if def.Type == "apiKey" && (def.Name == "" || def.In == "") {
				t.Errorf("apiKey scheme is incomplete: %+v", def)
			}
			// Every inferred definition has to admit it was inferred, since the
			// details are conventional rather than read from the code.
			if !strings.Contains(strings.ToLower(def.Description), "inferred") {
				t.Errorf("description does not admit it is inferred: %q", def.Description)
			}
		})
	}
}
