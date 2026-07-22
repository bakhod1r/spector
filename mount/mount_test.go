package mount

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
	"github.com/gofiber/fiber/v2"
	"github.com/gorilla/mux"
	"github.com/labstack/echo/v4"
	"github.com/user/specter"
)

// serveFunc issues one request against a mounted router and reports what came
// back, flattening the four frameworks' differing test entry points.
type serveFunc func(t *testing.T, method, path string) (status int, header http.Header, body string)

// mounters builds a router of each supported kind with the console mounted, so
// every test below runs against all of them. The point is that the frameworks
// are interchangeable from the caller's side: same paths, same redirect, same
// content — a framework whose helper drifts fails here rather than in a user's
// browser.
var mounters = map[string]func(specter.Config) serveFunc{
	"gin":        ginServer,
	"echo":       echoServer,
	"chi":        chiServer,
	"stdlib":     stdlibServer,
	"fiber":      fiberServer,
	"gorillamux": gorillaMuxServer,
}

func fromHandler(h http.Handler) serveFunc {
	return func(t *testing.T, method, path string) (int, http.Header, string) {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w.Code, w.Header(), w.Body.String()
	}
}

func ginServer(cfg specter.Config) serveFunc {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Gin(r, cfg)
	return fromHandler(r)
}

func echoServer(cfg specter.Config) serveFunc {
	e := echo.New()
	Echo(e, cfg)
	return fromHandler(e)
}

func chiServer(cfg specter.Config) serveFunc {
	r := chi.NewRouter()
	Chi(r, cfg)
	return fromHandler(r)
}

func gorillaMuxServer(cfg specter.Config) serveFunc {
	r := mux.NewRouter()
	GorillaMux(r, cfg)
	return fromHandler(r)
}

func stdlibServer(cfg specter.Config) serveFunc {
	mux := http.NewServeMux()
	Stdlib(mux, cfg)
	return fromHandler(mux)
}

// fiber runs on fasthttp, so it cannot be driven with httptest and uses its own
// in-process Test entry point.
func fiberServer(cfg specter.Config) serveFunc {
	app := fiber.New()
	Fiber(app, cfg)
	return func(t *testing.T, method, path string) (int, http.Header, string) {
		t.Helper()
		resp, err := app.Test(httptest.NewRequest(method, path, nil), -1)
		if err != nil {
			t.Fatalf("fiber: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, string(b)
	}
}

// forEach runs fn against every framework with the console mounted per cfg.
// Dir defaults to the repository root; a test that needs real application
// routes to exist sets it before calling.
func forEach(t *testing.T, cfg specter.Config, fn func(t *testing.T, serve serveFunc)) {
	t.Helper()
	if cfg.Dir == "" {
		cfg.Dir = ".."
	}
	cfg.Title, cfg.Version = "test", "0.0.1"
	for name, build := range mounters {
		t.Run(name, func(t *testing.T) { fn(t, build(cfg)) })
	}
}

// The bare mount point must redirect to the trailing-slash form, or the page's
// relative fetches resolve one level too high.
func TestRedirectsToTrailingSlash(t *testing.T) {
	forEach(t, specter.Config{}, func(t *testing.T, serve serveFunc) {
		status, header, _ := serve(t, http.MethodGet, "/docs")
		if status != http.StatusMovedPermanently {
			t.Fatalf("status = %d, want 301", status)
		}
		if loc := header.Get("Location"); loc != "/docs/" {
			t.Errorf("Location = %q, want /docs/", loc)
		}
	})
}

// probes are the query strings an endpoint needs before it will answer.
// /source legitimately 404s without one, so asking for a real file is the only
// way to tell "not routed" apart from "nothing to show".
var probes = map[string]string{
	"/source": "?file=specter.go&line=1",
}

// Every endpoint the console fetches must be routed. One missing would surface
// only as a broken pane in the browser.
func TestEveryEndpointIsReachable(t *testing.T) {
	forEach(t, specter.Config{}, func(t *testing.T, serve serveFunc) {
		for _, e := range endpoints {
			status, _, _ := serve(t, e.method, "/docs"+e.path+probes[e.path])
			if status == http.StatusNotFound {
				t.Errorf("%s /docs%s is not routed", e.method, e.path)
			}
		}
	})
}

func TestServesTheConsoleAsHTML(t *testing.T) {
	forEach(t, specter.Config{}, func(t *testing.T, serve serveFunc) {
		status, header, body := serve(t, http.MethodGet, "/docs/")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if ct := header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("Content-Type = %q, want HTML", ct)
		}
		if !strings.Contains(body, "<") {
			t.Error("body does not look like a page")
		}
	})
}

func TestServesTheSpec(t *testing.T) {
	forEach(t, specter.Config{}, func(t *testing.T, serve serveFunc) {
		status, _, body := serve(t, http.MethodGet, "/docs/openapi.json")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if !strings.Contains(body, `"openapi"`) {
			t.Errorf("body is not a spec: %.80s", body)
		}
	})
}

// ---- BasePath ----

// The console follows the configured path rather than a hardcoded /docs, and
// stops answering on the default once moved.
func TestCustomBasePath(t *testing.T) {
	forEach(t, specter.Config{BasePath: "/internal/api-docs"}, func(t *testing.T, serve serveFunc) {
		for _, path := range []string{"/", "/openapi.json", "/grpc.json", "/graphql.json"} {
			if status, _, _ := serve(t, http.MethodGet, "/internal/api-docs"+path); status != http.StatusOK {
				t.Errorf("%s: status = %d, want 200", path, status)
			}
		}
		if status, _, _ := serve(t, http.MethodGet, "/docs/"); status != http.StatusNotFound {
			t.Errorf("/docs/ still answers: status = %d, want 404", status)
		}
	})
}

func TestCustomBasePathRedirect(t *testing.T) {
	forEach(t, specter.Config{BasePath: "/internal/api-docs"}, func(t *testing.T, serve serveFunc) {
		status, header, _ := serve(t, http.MethodGet, "/internal/api-docs")
		if status != http.StatusMovedPermanently {
			t.Fatalf("status = %d, want 301", status)
		}
		if loc := header.Get("Location"); loc != "/internal/api-docs/" {
			t.Errorf("Location = %q, want the trailing-slash form", loc)
		}
	})
}

// A path written without a leading slash still mounts where the caller meant.
func TestBasePathWithoutLeadingSlash(t *testing.T) {
	forEach(t, specter.Config{BasePath: "console"}, func(t *testing.T, serve serveFunc) {
		if status, _, _ := serve(t, http.MethodGet, "/console/"); status != http.StatusOK {
			t.Errorf("status = %d, want 200", status)
		}
	})
}

// The handler routes on path suffixes, so a deep mount point must still have
// its prefix stripped before the handler sees the request.
func TestDeepBasePath(t *testing.T) {
	forEach(t, specter.Config{BasePath: "/a/b/c/docs"}, func(t *testing.T, serve serveFunc) {
		status, _, body := serve(t, http.MethodGet, "/a/b/c/docs/openapi.json")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if !strings.Contains(body, `"openapi"`) {
			t.Errorf("body is not a spec: %.80s", body)
		}
	})
}

// ---- access key ----

// A key arriving in the query must survive the redirect. Dropping it would
// bounce the first visit to a gated console into a 404 with no way back.
func TestRedirectKeepsQuery(t *testing.T) {
	forEach(t, specter.Config{AccessKey: "k"}, func(t *testing.T, serve serveFunc) {
		status, header, _ := serve(t, http.MethodGet, "/docs?key=k")
		if status != http.StatusMovedPermanently {
			t.Fatalf("status = %d, want 301", status)
		}
		if loc := header.Get("Location"); loc != "/docs/?key=k" {
			t.Errorf("Location = %q, want the key preserved", loc)
		}
	})
}

// Without the key every mounted route answers 404, including the invoke proxy,
// whatever the framework.
func TestGatedWithoutKey(t *testing.T) {
	forEach(t, specter.Config{AccessKey: "k"}, func(t *testing.T, serve serveFunc) {
		for _, e := range endpoints {
			if status, _, _ := serve(t, e.method, "/docs"+e.path); status != http.StatusNotFound {
				t.Errorf("%s /docs%s: status = %d, want 404", e.method, e.path, status)
			}
		}
	})
}

func TestGatedWithKey(t *testing.T) {
	forEach(t, specter.Config{AccessKey: "k"}, func(t *testing.T, serve serveFunc) {
		if status, _, _ := serve(t, http.MethodGet, "/docs/?key=k"); status != http.StatusOK {
			t.Errorf("status = %d, want 200", status)
		}
	})
}
