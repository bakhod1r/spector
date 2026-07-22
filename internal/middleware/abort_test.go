package middleware

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/user/specter/internal/core"
)

// chainFor parses src and reports what the index makes of the middleware
// registered on router "r". Working from source text rather than a fixture
// directory keeps a framework's spelling next to the assertion about it.
func chainFor(t *testing.T, src string) Chain {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "src.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex()
	ix.Collect(file)

	// A position past everything, so every Use call counts as being in effect.
	return ix.For("r", file.End(), nil)
}

func only(t *testing.T, c Chain, name string) core.Middleware {
	t.Helper()
	for _, m := range c {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("middleware %q not in chain %v", name, c)
	return core.Middleware{}
}

func hasStatus(m core.Middleware, code int) bool {
	for _, s := range m.Statuses {
		if s == code {
			return true
		}
	}
	return false
}

// net/http and chi reject with http.Error, whose status is the third argument.
func TestHTTPErrorIsARejection(t *testing.T) {
	got := only(t, chainFor(t, `package p

func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Tenant") == "" {
			http.Error(w, "no tenant", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func register() { r.Use(guard) }
`), "guard")

	if !hasStatus(got, 401) {
		t.Errorf("statuses = %v, want 401 from http.Error", got.Statuses)
	}
}

// A bare WriteHeader followed by a return is the other net/http spelling.
func TestWriteHeaderThenReturnIsARejection(t *testing.T) {
	got := only(t, chainFor(t, `package p

func apiKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func register() { r.Use(apiKey) }
`), "apiKey")

	if !hasStatus(got, 403) {
		t.Errorf("statuses = %v, want 403", got.Statuses)
	}
}

// A WriteHeader that is not a rejection must not be reported as one: the
// middleware carries on, so the status belongs to the handler's own responses.
func TestWriteHeaderWithoutReturnIsNotARejection(t *testing.T) {
	got := only(t, chainFor(t, `package p

func timing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		next.ServeHTTP(w, r)
	})
}

func register() { r.Use(timing) }
`), "timing")

	if len(got.Statuses) != 0 {
		t.Errorf("statuses = %v, want none — the middleware does not refuse the request", got.Statuses)
	}
}

// echo returns an error rather than writing one.
func TestEchoNewHTTPErrorIsARejection(t *testing.T) {
	got := only(t, chainFor(t, `package p

func tenantGuard(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().Header.Get("X-Tenant") == "" {
			return echo.NewHTTPError(http.StatusUnauthorized, "no tenant")
		}
		return next(c)
	}
}

func register() { r.Use(tenantGuard) }
`), "tenantGuard")

	if !hasStatus(got, 401) {
		t.Errorf("statuses = %v, want 401 from echo.NewHTTPError", got.Statuses)
	}
	if len(got.Headers) != 1 || got.Headers[0] != "X-Tenant" {
		t.Errorf("headers = %v, want [X-Tenant]", got.Headers)
	}
}

// A returned echo response is a rejection; the same call without a return is
// the middleware answering and continuing, which is not.
func TestEchoReturnedResponseIsARejection(t *testing.T) {
	got := only(t, chainFor(t, `package p

func csrf(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().Header.Get("X-CSRF-Token") == "" {
			return c.NoContent(http.StatusForbidden)
		}
		return next(c)
	}
}

func register() { r.Use(csrf) }
`), "csrf")

	if !hasStatus(got, 403) {
		t.Errorf("statuses = %v, want 403 from a returned c.NoContent", got.Statuses)
	}
}

// gin's spelling must keep working exactly as it did.
func TestGinAbortStillRecognised(t *testing.T) {
	got := only(t, chainFor(t, `package p

func authRequired(c *gin.Context) {
	if c.GetHeader("Authorization") == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no token"})
		return
	}
	c.Next()
}

func register() { r.Use(authRequired) }
`), "authRequired")

	if !hasStatus(got, 401) {
		t.Errorf("statuses = %v, want 401", got.Statuses)
	}
	if got.Kind != KindAuth {
		t.Errorf("kind = %q, want auth", got.Kind)
	}
}
