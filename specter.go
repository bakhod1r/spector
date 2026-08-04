package specter

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/bakhod1r/synth"
	"github.com/user/specter/internal/coverage"
	"github.com/user/specter/internal/export"
	"github.com/user/specter/internal/testgen"
	"gopkg.in/yaml.v3"

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
	"github.com/user/specter/internal/evolve"
	"github.com/user/specter/internal/gen"
	"github.com/user/specter/internal/gqlgenx"
	"github.com/user/specter/internal/graphqlsdl"
	"github.com/user/specter/internal/grpcx"
	"github.com/user/specter/internal/lint"
	"github.com/user/specter/internal/middleware"
	"github.com/user/specter/internal/mock"
	"github.com/user/specter/internal/pbgo"
	"github.com/user/specter/internal/proto"
	"github.com/user/specter/internal/proxy"
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

// ProxyOptions configures the traffic-verifying proxy.
type ProxyOptions = proxy.Options

// Proxy is a running comparison between a document and live traffic.
type Proxy = proxy.Proxy

// NewProxy builds a reverse proxy that forwards to opts.Target and reports
// where the traffic passing through it disagrees with doc.
//
// The contract artefacts check a document with requests Specter invented; this
// checks it with the requests real clients actually make — the empty lists, the
// error paths, and the endpoints the scanner never saw. It forwards everything
// untouched: it is a watcher, not a gate, and no finding is worth degrading the
// API being observed.
func NewProxy(doc *Document, opts ProxyOptions) (*Proxy, error) {
	return proxy.New(doc, opts)
}

// EvolveChange is one difference between two versions of a document, classified
// by its effect on an existing client.
type EvolveChange = evolve.Change

// EvolveOptions selects what the current document is compared against. Exactly
// one source is set.
type EvolveOptions struct {
	// SinceRev compares against a git revision (HEAD~1, v1.0.0, main): the
	// revision is exported to a temp directory and scanned, so the working tree
	// is never touched.
	SinceRev string
	// BaselineDir compares against another source directory.
	BaselineDir string
	// BaselineJSON compares against an existing openapi.json.
	BaselineJSON string
}

// Evolve compares the current document against a baseline and reports what
// changed, classified as breaking, compatible, or an addition.
//
// The question it answers is the one a version bump is meant to encode and
// rarely does: is this safe to ship? Breaking means an existing, working client
// stops working — not merely that the document differs. The baseline is scanned
// exactly as the current document is, so a change in how Specter reads code
// affects both sides equally and does not masquerade as an API change.
func Evolve(cfg Config, opts EvolveOptions) ([]EvolveChange, error) {
	newDoc, err := Generate(cfg)
	if err != nil {
		return nil, err
	}
	oldDoc, err := baselineDoc(cfg, opts)
	if err != nil {
		return nil, err
	}
	return evolve.Compare(oldDoc, newDoc), nil
}

// baselineDoc produces the document to compare against, from whichever of the
// three sources is set.
func baselineDoc(cfg Config, opts EvolveOptions) (*Document, error) {
	set := 0
	for _, s := range []string{opts.SinceRev, opts.BaselineDir, opts.BaselineJSON} {
		if s != "" {
			set++
		}
	}
	if set == 0 {
		return nil, fmt.Errorf("no baseline: set one of a git revision (-since), a directory (-baseline-dir), or a document (-baseline)")
	}
	if set > 1 {
		return nil, fmt.Errorf("more than one baseline given: choose one of -since, -baseline-dir, or -baseline")
	}

	switch {
	case opts.BaselineJSON != "":
		data, err := os.ReadFile(opts.BaselineJSON)
		if err != nil {
			return nil, err
		}
		var doc core.Document
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", opts.BaselineJSON, err)
		}
		return &doc, nil
	case opts.BaselineDir != "":
		baseCfg := cfg
		baseCfg.Dir = opts.BaselineDir
		return Generate(baseCfg)
	default:
		return revisionDoc(cfg, opts.SinceRev)
	}
}

// revisionDoc scans the directory as it was at a git revision.
//
// The revision is exported with `git archive` into a temp directory rather than
// checked out, so the working tree, the index, and any uncommitted work are
// untouched — the comparison has no side effects on the repository it reads.
func revisionDoc(cfg Config, rev string) (*Document, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git is required to compare against a revision, and it was not found on PATH")
	}

	tmp, err := os.MkdirTemp("", "specter-since-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	// The archive is scoped to the scanned directory and streamed through tar,
	// so only the sources that matter are written out.
	dir := cfg.Dir
	if dir == "" {
		dir = "."
	}
	archive := exec.Command("git", "archive", rev, "--", dir)
	extract := exec.Command("tar", "-x", "-C", tmp)

	pipe, err := archive.StdoutPipe()
	if err != nil {
		return nil, err
	}
	extract.Stdin = pipe
	var archiveErr strings.Builder
	archive.Stderr = &archiveErr

	if err := extract.Start(); err != nil {
		return nil, err
	}
	if err := archive.Start(); err != nil {
		return nil, err
	}
	if err := archive.Wait(); err != nil {
		return nil, fmt.Errorf("git archive %s: %w: %s", rev, err, strings.TrimSpace(archiveErr.String()))
	}
	if err := extract.Wait(); err != nil {
		return nil, fmt.Errorf("extracting %s: %w", rev, err)
	}

	baseCfg := cfg
	baseCfg.Dir = filepath.Join(tmp, dir)
	return Generate(baseCfg)
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

// GenerateSDKFromDocument builds a client from an OpenAPI document that already
// exists, rather than by scanning source. This is the path for an API described
// by a hand-written or third-party openapi.json/yaml: load it with
// LoadDocument, then generate a client in any supported language.
func GenerateSDKFromDocument(doc *core.Document, opts SDKOptions) ([]SDKFile, error) {
	if doc == nil {
		return nil, fmt.Errorf("sdk: nil document")
	}
	return sdk.Generate(doc, opts)
}

// LoadDocument reads an OpenAPI document from a JSON or YAML file into the model
// the generators consume. The format is chosen by extension (.yaml/.yml are
// YAML, anything else JSON); YAML is bridged through JSON so the document's
// existing JSON field names are honoured either way.
func LoadDocument(path string) (*core.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".yaml" || ext == ".yml" {
		var any interface{}
		if err := yaml.Unmarshal(data, &any); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if data, err = json.Marshal(normalizeYAML(any)); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	var doc core.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &doc, nil
}

// normalizeYAML turns the map[interface{}]interface{} that a YAML decoder can
// produce into the map[string]interface{} that json.Marshal requires, walking
// nested maps and slices.
func normalizeYAML(v interface{}) interface{} {
	switch t := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, val := range t {
			m[fmt.Sprint(k)] = normalizeYAML(val)
		}
		return m
	case map[string]interface{}:
		for k, val := range t {
			t[k] = normalizeYAML(val)
		}
		return t
	case []interface{}:
		for i, val := range t {
			t[i] = normalizeYAML(val)
		}
		return t
	default:
		return v
	}
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

// ExportPostmanEnvironment renders a fillable Postman environment carrying the
// collection's variables (baseUrl and auth placeholders), so one collection can
// be pointed at dev, staging or prod by switching environments.
func ExportPostmanEnvironment(doc *Document) ([]byte, error) {
	return export.PostmanEnvironment(doc)
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
		// synth/body generates a realistic request body for one operation from
		// the document's own schema, using github.com/bakhod1r/synth. The
		// console offers it behind a "Generate body" button so a caller can
		// fill a request with plausible data instead of the bare sample.
		if strings.HasSuffix(r.URL.Path, "synth/body") {
			method, path := r.URL.Query().Get("method"), r.URL.Query().Get("path")
			if method == "" || path == "" {
				http.Error(w, "method and path are required", http.StatusBadRequest)
				return
			}
			specBytes, merr := json.Marshal(doc)
			if merr != nil {
				writeJSON(w, map[string]string{"error": merr.Error()})
				return
			}
			api, aerr := synth.OpenAPIBytes(specBytes)
			if aerr != nil {
				writeJSON(w, map[string]string{"error": aerr.Error()})
				return
			}
			body, perr := api.PayloadJSON(method, path)
			if perr != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				writeJSON(w, map[string]string{"error": perr.Error()})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
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
