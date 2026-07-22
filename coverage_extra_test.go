package specter

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/ui"
)

const fiberSrc = `package app

import "github.com/gofiber/fiber/v2"

func Register(app *fiber.App) {
	app.Get("/things", nil)
}
`

const gorillaSrc = `package app

import "github.com/gorilla/mux"

func Register(r *mux.Router) {
	r.HandleFunc("/things", nil).Methods("GET")
}
`

func TestDetectFiberAndGorilla(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"fiber", fiberSrc, "fiber"},
		{"gorilla", gorillaSrc, "gorillamux"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTree(t, map[string]string{"main.go": tc.src})
			if got := detect(dir); got != tc.want {
				t.Errorf("detect = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAdapterForFiberAndGorilla(t *testing.T) {
	if a := adapterFor(Config{Adapter: "fiber"}); a == nil || a.Name() != "fiber" {
		t.Errorf("adapterFor(fiber) = %v", a)
	}
	for _, name := range []string{"gorillamux", "mux", "gorilla"} {
		if a := adapterFor(Config{Adapter: name}); a == nil || a.Name() != "gorillamux" {
			t.Errorf("adapterFor(%q) = %v", name, a)
		}
	}
}

func TestGenerateSDK(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	files, err := GenerateSDK(Config{Dir: dir}, SDKOptions{Lang: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Error("GenerateSDK produced no files")
	}
}

func TestGenerateSDKMissingDirErrors(t *testing.T) {
	if _, err := GenerateSDK(Config{Dir: filepath.Join(t.TempDir(), "nope")}, SDKOptions{}); err == nil {
		t.Error("GenerateSDK on a missing dir returned no error")
	}
}

func TestScanRoutes(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	routes, err := ScanRoutes(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Errorf("routes = %+v, want the two widget routes", routes)
	}
}

func TestLint(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	routes, err := ScanRoutes(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lint(Config{Dir: dir}, routes); err != nil {
		t.Fatalf("Lint: %v", err)
	}
}

func TestMockHandlerServesDocumentedPath(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	doc, err := Generate(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	h := MockHandler(doc, MockOptions{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/widgets", nil))
	if rec.Code >= 500 {
		t.Errorf("mock returned %d", rec.Code)
	}
}

func TestServeMockBadAddrErrors(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	doc, err := Generate(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := ServeMock("this is not an address", doc, MockOptions{}); err == nil {
		t.Error("ServeMock on a bad address returned no error")
	}
}

func TestGenerateAdmin(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	files, err := GenerateAdmin(Config{Dir: dir}, AdminOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Error("GenerateAdmin produced no files")
	}
}

func TestGenerateAdminMissingDirErrors(t *testing.T) {
	if _, err := GenerateAdmin(Config{Dir: filepath.Join(t.TempDir(), "nope")}, AdminOptions{}); err == nil {
		t.Error("GenerateAdmin on a missing dir returned no error")
	}
}

func TestAdminModel(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	if _, err := AdminModel(Config{Dir: dir}); err != nil {
		t.Fatal(err)
	}
}

func TestAdminModelMissingDirErrors(t *testing.T) {
	if _, err := AdminModel(Config{Dir: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Error("AdminModel on a missing dir returned no error")
	}
}

func TestApplyInferredSchemes(t *testing.T) {
	route := func(scheme string) core.Route {
		return core.Route{Middleware: []core.Middleware{{Name: "mw", Scheme: scheme}}}
	}

	t.Run("no schemes is a no-op", func(t *testing.T) {
		doc := core.NewDocument("t", "v")
		applyInferredSchemes(doc, []core.Route{{}, route("")})
		if doc.Components.SecuritySchemes != nil {
			t.Errorf("schemes = %v, want none", doc.Components.SecuritySchemes)
		}
	})

	t.Run("inferred schemes are defined", func(t *testing.T) {
		doc := core.NewDocument("t", "v")
		applyInferredSchemes(doc, []core.Route{route("bearerAuth"), route("apiKey")})
		for _, want := range []string{"bearerAuth", "apiKey"} {
			if doc.Components.SecuritySchemes[want] == nil {
				t.Errorf("scheme %q not defined; got %v", want, doc.Components.SecuritySchemes)
			}
		}
	})

	t.Run("declared scheme is left alone", func(t *testing.T) {
		doc := core.NewDocument("t", "v")
		declared := &core.SecurityScheme{Type: "apiKey", Name: "X-Mine", In: "header"}
		doc.Components.SecuritySchemes = map[string]*core.SecurityScheme{"bearerAuth": declared}
		applyInferredSchemes(doc, []core.Route{route("bearerAuth")})
		if doc.Components.SecuritySchemes["bearerAuth"] != declared {
			t.Error("inference overwrote a declared scheme")
		}
	})
}

func TestPageWith(t *testing.T) {
	t.Run("no admin url serves the page unchanged", func(t *testing.T) {
		if got := pageWith(Config{}); !bytes.Equal(got, ui.Page) {
			t.Error("page was rewritten without an AdminURL")
		}
	})

	t.Run("admin url is injected before </head>", func(t *testing.T) {
		got := string(pageWith(Config{AdminURL: "http://admin.example"}))
		if !strings.Contains(got, `window.__specter={"adminUrl":"http://admin.example"}`) {
			t.Errorf("config script missing from page")
		}
		if !strings.Contains(got, "</head>") {
			t.Error("</head> lost during injection")
		}
	})

	t.Run("page without head is served unchanged", func(t *testing.T) {
		orig := ui.Page
		defer func() { ui.Page = orig }()
		ui.Page = []byte("<body>no head</body>")
		if got := pageWith(Config{AdminURL: "x"}); !bytes.Equal(got, ui.Page) {
			t.Errorf("page = %q, want it unchanged when </head> is absent", got)
		}
	})
}
