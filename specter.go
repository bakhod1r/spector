package specter

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/user/specter/internal/coverage"
	"github.com/user/specter/internal/export"
	"github.com/user/specter/internal/testgen"

	chiadapter "github.com/user/specter/internal/adapter/chi"
	echoadapter "github.com/user/specter/internal/adapter/echo"
	fiberadapter "github.com/user/specter/internal/adapter/fiber"
	ginadapter "github.com/user/specter/internal/adapter/gin"
	gorillamuxadapter "github.com/user/specter/internal/adapter/gorillamux"
	stdlibadapter "github.com/user/specter/internal/adapter/stdlib"
	"github.com/user/specter/internal/admin"
	"github.com/user/specter/internal/advice"
	"github.com/user/specter/internal/contract"
	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/gen"
	"github.com/user/specter/internal/gqlgenx"
	"github.com/user/specter/internal/graphqlsdl"
	"github.com/user/specter/internal/grpcx"
	"github.com/user/specter/internal/lint"
	"github.com/user/specter/internal/middleware"
	"github.com/user/specter/internal/mock"
	"github.com/user/specter/internal/pbgo"
	"github.com/user/specter/internal/proto"
	"github.com/user/specter/internal/sdk"
	"github.com/user/specter/internal/source"
	"github.com/user/specter/internal/ui"
)

// Server and SecurityScheme are declared through Config, so they need names
// callers can reach. The models live in an internal package; these aliases
// expose them without exposing the package.
type (
	// Server is one base URL the API is reachable at.
	Server = core.Server
	// SecurityScheme describes how a caller authenticates. Type is "http"
	// (with Scheme "bearer" or "basic") or "apiKey" (with Name and In).
	SecurityScheme = core.SecurityScheme

	// Document, GrpcDoc and GraphqlDoc are what the Generate functions return.
	// Aliasing them lets callers name the type they were handed.
	Document   = core.Document
	GrpcDoc    = core.GrpcDoc
	GraphqlDoc = core.GraphqlDoc
	Schema     = core.Schema
	Operation  = core.Operation
)

type Config struct {
	Dir        string
	Title      string
	Version    string
	Adapter    string
	ProtoDir   string
	GraphqlDir string

	// Servers are the base URLs the API is reachable at. They cannot be
	// inferred from source, so declare them when the document is consumed by
	// codegen or a client that needs an absolute URL.
	Servers []Server

	// Security declares how callers authenticate, keyed by scheme name.
	// Whether a route is protected is decided by middleware, which the AST
	// cannot follow, so this applies to the document as a whole: every
	// declared scheme is listed as an alternative. Per-operation requirements
	// are not inferred.
	Security map[string]SecurityScheme

	// BasePath is where the console is mounted. Empty means "/docs".
	// A leading slash is added and a trailing one removed, so "docs",
	// "/docs" and "/docs/" all mean the same thing.
	BasePath string

	// AdminURL, when set, adds an "Admin panel" button to the console that
	// links there. It is only a link — the panel authenticates on its own, so
	// pointing at it does not expose it. Empty hides the button.
	AdminURL string

	// AccessKey gates the console behind a shared secret. Empty (the default)
	// serves it to anyone who can reach the route.
	//
	// This keeps the console off an internet-facing deployment; it is not user
	// authentication. There are no accounts, no expiry, and no revocation
	// beyond changing the value and restarting. Anyone holding the key has the
	// same access as anyone else, and the console can invoke your gRPC methods,
	// so treat it as a deployment secret rather than a login.
	AccessKey string
}

// DefaultBasePath is where the console lives unless Config says otherwise.
const DefaultBasePath = "/docs"

// BasePathOrDefault normalizes the configured mount point: always a leading
// slash, never a trailing one, so callers can join paths without guessing.
func (c Config) BasePathOrDefault() string {
	p := strings.TrimSpace(c.BasePath)
	if p == "" {
		return DefaultBasePath
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	if p == "/" {
		// Mounting at the site root would swallow every route on the router.
		return DefaultBasePath
	}
	return p
}

// accessCookie carries a key accepted from the query string so the console's
// own relative fetches (openapi.json, grpc.json, …) do not each need it.
const accessCookie = "specter_key"

// authorized reports whether a request carries the configured key. The
// comparison is constant-time so a caller cannot recover the key by measuring
// how long a wrong guess takes.
func authorized(r *http.Request, key string) bool {
	if key == "" {
		return true
	}
	if v := r.Header.Get("X-Specter-Key"); v != "" &&
		subtle.ConstantTimeCompare([]byte(v), []byte(key)) == 1 {
		return true
	}
	if v := r.URL.Query().Get("key"); v != "" &&
		subtle.ConstantTimeCompare([]byte(v), []byte(key)) == 1 {
		return true
	}
	if c, err := r.Cookie(accessCookie); err == nil &&
		subtle.ConstantTimeCompare([]byte(c.Value), []byte(key)) == 1 {
		return true
	}
	return false
}

func (c Config) withDefaults() Config {
	if c.Dir == "" {
		c.Dir = "."
	}
	if c.Title == "" {
		c.Title = filepath.Base(mustAbs(c.Dir))
	}
	if c.Version == "" {
		c.Version = "0.1.0"
	}
	return c
}

func mustAbs(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

// adapterFor never fails: an unrecognised name falls back to gin rather than
// erroring, so there is nothing for a caller to handle.
func adapterFor(cfg Config) core.Adapter {
	name := cfg.Adapter
	if name == "" {
		name = detect(cfg.Dir)
	}
	switch name {
	case "chi":
		return &chiadapter.Adapter{}
	case "echo":
		return &echoadapter.Adapter{}
	case "fiber":
		return &fiberadapter.Adapter{}
	case "gorillamux", "mux", "gorilla":
		return &gorillamuxadapter.Adapter{}
	case "stdlib":
		return &stdlibadapter.Adapter{}
	default:
		return &ginadapter.Adapter{}
	}
}

func detect(dir string) string {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ImportsOnly)
	if err != nil {
		return "gin"
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				switch {
				case strings.Contains(p, "gin-gonic/gin"):
					return "gin"
				case strings.Contains(p, "go-chi/chi"):
					return "chi"
				case strings.Contains(p, "labstack/echo"):
					return "echo"
				case strings.Contains(p, "gofiber/fiber"):
					return "fiber"
				case strings.Contains(p, "gorilla/mux"):
					return "gorillamux"
				}
			}
		}
	}
	return "stdlib"
}

// Route and Finding are what ScanRoutes and Lint deal in; aliased so callers
// outside the module can name them.
type (
	Route   = core.Route
	Finding = lint.Finding
)

// ScanRoutes returns the routes an adapter finds, without building a document.
// Lint needs them, and so does anything else that wants the raw result of the
// scan rather than OpenAPI.
func ScanRoutes(cfg Config) ([]Route, error) {
	cfg = cfg.withDefaults()
	routes, _, err := adapterFor(cfg).Scan(cfg.Dir)
	return routes, err
}

// Lint reports routing problems that compile cleanly and fail silently: a
// handler nothing registers, a path registered twice, a literal path shadowed
// by a parameterised one. Pass the routes from ScanRoutes.
func Lint(cfg Config, routes []Route) ([]Finding, error) {
	return lint.Analyze(cfg.withDefaults().Dir, routes)
}

// MockOptions configures the mock server, principally its CORS policy. The mock
// runs on its own port, so every browser call to it is cross-origin and these
// headers decide whether a frontend can reach it at all.
type MockOptions = mock.Options

// MockHandler serves a generated document as a working API: every documented
// path answers with a body that satisfies its own response schema.
//
// It is shape, not state. Two GETs return the same body and a POST does not
// change what a later GET returns, because the document does not describe that
// behaviour and guessing at it would make the mock confidently wrong.
func MockHandler(doc *Document, opts MockOptions) http.Handler {
	return mock.HandlerWith(doc, opts)
}

// ServeMock runs the mock on addr until the process stops.
func ServeMock(addr string, doc *Document, opts MockOptions) error {
	return http.ListenAndServe(addr, MockHandler(doc, opts))
}

// AdminOptions configures the generated admin panel.
type AdminOptions = admin.Options

// AdminFile is one generated file, named relative to the output directory.
type AdminFile = admin.File

// GenerateAdmin builds an admin panel from the scanned API: a master list per
// resource, a read-only detail view, and per-row actions limited to the
// operations the API actually has.
//
// It returns source rather than serving anything. An admin panel is where
// per-project judgement lives — which column matters, which field is a secret,
// what a status value means — and none of that is in an OpenAPI document. The
// generated code is a starting point that compiles, not a framework to
// configure.
func GenerateAdmin(cfg Config, opts AdminOptions) ([]AdminFile, error) {
	doc, err := Generate(cfg)
	if err != nil {
		return nil, err
	}
	return admin.Generate(doc, opts)
}

// ContractOptions configures the generated contract artefacts.
type ContractOptions = contract.Options

// ContractFile is one generated file, named relative to the output directory.
type ContractFile = contract.File

// GenerateContract builds artefacts that exercise the API against its own
// document: a .http file for the editor, Go tests for CI, and a shell smoke
// test for a pipeline with nothing installed.
//
// A generated document is a claim, and until it is executed nothing checks it.
// A document and the service it describes drift apart quietly, which is the one
// failure that makes documentation worse than none — it is believed. These
// artefacts are what makes the drift fail a build instead.
//
// Like the admin panel, the output is source rather than a runtime: the first
// version is free and every version after it belongs to the project.
func GenerateContract(cfg Config, opts ContractOptions) ([]ContractFile, error) {
	doc, err := Generate(cfg)
	if err != nil {
		return nil, err
	}
	return contract.Generate(doc, opts)
}

// AdminModel reports the resources GenerateAdmin would produce, without
// generating anything. It answers "what would the panel contain?" — useful for
// a dry run before writing files into a project.
func AdminModel(cfg Config) (admin.Model, error) {
	doc, err := Generate(cfg)
	if err != nil {
		return admin.Model{}, err
	}
	return admin.Build(doc), nil
}

// SDKOptions configures the client generator: language ("ts" or "go"),
// package name, and base URL.
type SDKOptions = sdk.Options

// SDKFile is one generated file, named relative to the output directory.
type SDKFile = sdk.File

// GenerateSDK builds a typed client for the scanned API in the requested
// language. Like GenerateAdmin it returns source rather than serving anything:
// the output has no dependency beyond the standard library (net/http, fetch)
// and is meant to be committed and edited.
func GenerateSDK(cfg Config, opts SDKOptions) ([]SDKFile, error) {
	doc, err := Generate(cfg)
	if err != nil {
		return nil, err
	}
	return sdk.Generate(doc, opts)
}

func Generate(cfg Config) (*core.Document, error) {
	cfg = cfg.withDefaults()
	routes, schemas, err := adapterFor(cfg).Scan(cfg.Dir)
	if err != nil {
		return nil, err
	}
	doc := gen.Build(cfg.Title, cfg.Version, routes, schemas)
	applyInferredSchemes(doc, routes)
	applyDeclared(doc, cfg)
	applyAdvice(doc)
	return doc, nil
}

// applyAdvice attaches standards recommendations to each operation. They are
// advisory only: nothing about the document changes, so a consumer that
// ignores the extension sees exactly what it saw before.
func applyAdvice(doc *core.Document) {
	for key, list := range advice.Review(doc) {
		method, path, ok := strings.Cut(key, " ")
		if !ok {
			continue
		}
		if op := doc.Paths[path][strings.ToLower(method)]; op != nil {
			op.Advice = list
		}
	}
}

// applyInferredSchemes defines the security schemes that middleware implied.
//
// Without this the document would reference schemes it never defines, which is
// not merely untidy: a security requirement naming an undefined scheme makes
// the document invalid, and every consumer rejects it.
//
// A scheme declared in Config is left alone. The inferred definitions are
// conventional — a middleware called JWTAuth does not say where the token goes —
// so anything the operator states explicitly is better evidence than a guess.
func applyInferredSchemes(doc *core.Document, routes []core.Route) {
	names := map[string]bool{}
	for _, r := range routes {
		for _, m := range r.Middleware {
			if m.Scheme != "" {
				names[m.Scheme] = true
			}
		}
	}
	if len(names) == 0 {
		return
	}
	if doc.Components.SecuritySchemes == nil {
		doc.Components.SecuritySchemes = map[string]*core.SecurityScheme{}
	}

	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted) // map order is random; the document must be reproducible

	for _, name := range sorted {
		if _, declared := doc.Components.SecuritySchemes[name]; declared {
			continue
		}
		def := middleware.SchemeDefinition(name)
		doc.Components.SecuritySchemes[name] = &def
	}
}

// applyDeclared copies the parts of the document that cannot be read from
// source: which servers host the API, and how callers authenticate.
func applyDeclared(doc *core.Document, cfg Config) {
	if len(cfg.Servers) > 0 {
		doc.Servers = append(doc.Servers, cfg.Servers...)
	}
	if len(cfg.Security) == 0 {
		return
	}

	doc.Components.SecuritySchemes = map[string]*core.SecurityScheme{}
	names := make([]string, 0, len(cfg.Security))
	for name := range cfg.Security {
		names = append(names, name)
	}
	// Map iteration order is random; the document has to be reproducible so a
	// regenerated spec does not churn in review.
	sort.Strings(names)

	for _, name := range names {
		scheme := cfg.Security[name]
		doc.Components.SecuritySchemes[name] = &scheme
		// Separate entries are alternatives: any one satisfies the request.
		// Requiring several together would need per-route knowledge the AST
		// cannot supply.
		doc.Security = append(doc.Security, core.SecurityRequirement{name: []string{}})
	}
}

// findSourceDir locates the directory holding files with one of the given
// extensions, looking in root and then one level below it.
//
// Projects put protos and schemas in a subdirectory — proto/, graph/, schema/,
// api/ — and which one is a matter of taste. Requiring the caller to name it
// means the common case fails with "no services found" and nothing to act on,
// which is a poor way to learn that the guess was wrong. Falling back to root
// keeps the behaviour of an explicit setting unchanged.
func findSourceDir(root string, exts ...string) string {
	has := func(dir string) bool {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			for _, ext := range exts {
				if strings.HasSuffix(e.Name(), ext) {
					return true
				}
			}
		}
		return false
	}

	if has(root) {
		return root
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return root
	}
	// Sorted by ReadDir already, so the choice is deterministic when two
	// subdirectories both qualify.
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if candidate := filepath.Join(root, e.Name()); has(candidate) {
			return candidate
		}
	}
	return root
}

// GenerateGrpc builds a gRPC document. It prefers .proto sources and falls
// back to generated Go stubs (*.pb.go) when the protos yield no services, so
// projects that ship only generated code are still documented.
func GenerateGrpc(cfg Config) (*core.GrpcDoc, error) {
	cfg = cfg.withDefaults()
	dir := cfg.ProtoDir
	if dir == "" {
		dir = findSourceDir(cfg.Dir, ".proto")
	}
	doc, err := proto.Scan(dir)
	if err == nil && len(doc.Services) > 0 {
		return doc, nil
	}
	if pb, pberr := pbgo.Scan(dir); pberr == nil && len(pb.Services) > 0 {
		return pb, nil
	}
	return doc, err
}

// GenerateGraphql builds a GraphQL document. It prefers .graphql/.graphqls
// SDL sources and falls back to generated Go code (gqlgen resolver
// interfaces and models) when the SDL yields no queries, so projects that
// ship only generated code are still documented.
func GenerateGraphql(cfg Config) (*core.GraphqlDoc, error) {
	cfg = cfg.withDefaults()
	dir := cfg.GraphqlDir
	if dir == "" {
		dir = findSourceDir(cfg.Dir, ".graphql", ".graphqls")
	}
	doc, err := graphqlsdl.Scan(dir)
	if err == nil && (len(doc.Queries) > 0 || len(doc.Types) > 0) {
		return doc, nil
	}
	if gg, gerr := gqlgenx.Scan(dir); gerr == nil && (len(gg.Queries) > 0 || len(gg.Types) > 0) {
		return gg, nil
	}
	return doc, err
}

// ExportPostman renders the document as a Postman collection v2.1. Insomnia
// imports the same format, so one export serves both clients.
func ExportPostman(doc *Document) ([]byte, error) {
	return export.Postman(doc)
}

// ExportMarkdown renders the document as a static Markdown API reference,
// suitable for a README or a docs site.
func ExportMarkdown(doc *Document) []byte {
	return export.Markdown(doc)
}

// ToV31 converts the document to OpenAPI 3.1, returning a generic JSON tree
// because the 3.0 and 3.1 spellings of exclusive bounds cannot share a struct.
func ToV31(doc *Document) (map[string]any, error) {
	return doc.ToV31()
}

// TestgenOptions configures GenerateTests.
type TestgenOptions = testgen.Options

// GenerateTests writes a Go integration test file from the document: one test
// per operation, requests filled from examples and schemas, asserting the
// response status is documented. The tests target SPECTER_BASE_URL and skip
// when it is unset.
func GenerateTests(doc *Document, opts TestgenOptions) []byte {
	return testgen.Generate(doc, opts)
}

// CoverageReport is what MeasureCoverage returns.
type CoverageReport = coverage.Report

// MeasureCoverage reports how documented the API is: which operations lack a
// summary, a typed response, an error response, and so on, with an overall
// percentage.
func MeasureCoverage(doc *Document) CoverageReport {
	return coverage.Measure(doc)
}

func Handler(cfg Config) http.Handler {
	var (
		once sync.Once
		doc  *core.Document
		gdoc *core.GrpcDoc
		qdoc *core.GraphqlDoc
		err  error
		gerr error
		qerr error
	)
	build := func() {
		doc, err = Generate(cfg)
		gdoc, gerr = GenerateGrpc(cfg)
		qdoc, qerr = GenerateGraphql(cfg)
	}

	writeJSON := func(w http.ResponseWriter, v interface{}) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(v)
	}

	protoDir := func() string {
		c := cfg.withDefaults()
		if c.ProtoDir != "" {
			return c.ProtoDir
		}
		return c.Dir
	}()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, cfg.AccessKey) {
			// 404 rather than 401: a gated console should not confirm it is
			// there to someone without the key.
			http.NotFound(w, r)
			return
		}
		// A key that arrived in the URL becomes a cookie so the page's own
		// fetches carry it, and so it stops being echoed in every link.
		if cfg.AccessKey != "" && r.URL.Query().Get("key") != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     accessCookie,
				Value:    cfg.AccessKey,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   r.TLS != nil,
			})
		}

		once.Do(build)
		if strings.HasSuffix(r.URL.Path, "grpc/invoke") {
			if r.Method != http.MethodPost {
				http.Error(w, "POST required", http.StatusMethodNotAllowed)
				return
			}
			var req grpcx.Request
			if derr := json.NewDecoder(r.Body).Decode(&req); derr != nil {
				writeJSON(w, map[string]string{"error": derr.Error()})
				return
			}
			resp, ierr := grpcx.Invoke(protoDir, req)
			if ierr != nil {
				w.WriteHeader(http.StatusBadGateway)
				writeJSON(w, map[string]string{"error": ierr.Error(), "response": resp})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(resp))
			return
		}
		if strings.HasSuffix(r.URL.Path, "source") {
			line, _ := strconv.Atoi(r.URL.Query().Get("line"))
			snip, serr := source.Read(cfg.withDefaults().Dir, r.URL.Query().Get("file"), line)
			if serr != nil {
				// The reason is deliberately not reported: distinguishing "no
				// such file" from "outside the tree" tells a caller which
				// guesses are getting closer.
				http.Error(w, "not available", http.StatusNotFound)
				return
			}
			writeJSON(w, snip)
			return
		}
		if strings.HasSuffix(r.URL.Path, "grpc.json") {
			if gerr != nil || gdoc == nil {
				writeJSON(w, core.NewGrpcDoc())
				return
			}
			writeJSON(w, gdoc)
			return
		}
		if strings.HasSuffix(r.URL.Path, "graphql.json") {
			if qerr != nil || qdoc == nil {
				writeJSON(w, core.NewGraphqlDoc())
				return
			}
			writeJSON(w, qdoc)
			return
		}
		if err != nil {
			http.Error(w, "specter: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if strings.HasSuffix(r.URL.Path, "openapi.json") {
			writeJSON(w, doc)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(pageWith(cfg))
	})
}

// pageWith injects runtime settings the console cannot infer — currently just
// the admin panel URL — as a script tag before </head>. When there is nothing
// to inject the embedded page is served unchanged.
func pageWith(cfg Config) []byte {
	if cfg.AdminURL == "" {
		return ui.Page
	}
	cfgJSON, err := json.Marshal(map[string]string{"adminUrl": cfg.AdminURL})
	if err != nil {
		return ui.Page
	}
	tag := []byte("<script>window.__specter=" + string(cfgJSON) + "</script></head>")
	if i := bytes.Index(ui.Page, []byte("</head>")); i >= 0 {
		out := make([]byte, 0, len(ui.Page)+len(tag))
		out = append(out, ui.Page[:i]...)
		out = append(out, tag...)
		out = append(out, ui.Page[i+len("</head>"):]...)
		return out
	}
	return ui.Page
}
