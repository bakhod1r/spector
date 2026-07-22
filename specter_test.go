package specter

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"

	"github.com/user/specter/examples/shop/shoppb"
	"github.com/user/specter/internal/core"
)

// writeTree materialises a small module on disk so the AST-based scanners have
// something real to walk; they read files, not in-memory sources.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const ginSrc = `package app

import "github.com/gin-gonic/gin"

type Widget struct {
	ID   int    ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\"`" + `
}

func Register(r *gin.Engine) {
	r.GET("/widgets", func(c *gin.Context) {})
	r.POST("/widgets", func(c *gin.Context) {})
}
`

const echoSrc = `package app

import "github.com/labstack/echo/v4"

func Register(e *echo.Echo) {
	e.GET("/things", nil)
}
`

const chiSrc = `package app

import "github.com/go-chi/chi/v5"

func Register(r chi.Router) {
	r.Get("/things", nil)
}
`

const stdlibSrc = `package app

import "net/http"

func Register(mux *http.ServeMux) {
	mux.HandleFunc("/plain", func(w http.ResponseWriter, r *http.Request) {})
}
`

func TestWithDefaults(t *testing.T) {
	got := Config{}.withDefaults()
	if got.Dir != "." {
		t.Errorf("Dir = %q, want %q", got.Dir, ".")
	}
	if got.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", got.Version, "0.1.0")
	}
	// Title defaults to the directory name, so it must not be left empty.
	if got.Title == "" {
		t.Error("Title was not defaulted")
	}

	explicit := Config{Dir: "x", Title: "T", Version: "9"}.withDefaults()
	if explicit.Dir != "x" || explicit.Title != "T" || explicit.Version != "9" {
		t.Errorf("defaults overwrote explicit values: %+v", explicit)
	}
}

func TestMustAbs(t *testing.T) {
	if got := mustAbs("."); !filepath.IsAbs(got) {
		t.Errorf("mustAbs(%q) = %q, want an absolute path", ".", got)
	}
}

// detect picks the adapter from the imports actually present in the tree.
func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"gin", ginSrc, "gin"},
		{"chi", chiSrc, "chi"},
		{"echo", echoSrc, "echo"},
		{"no known router", stdlibSrc, "stdlib"},
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

// An unparseable directory must not panic; gin is the documented fallback.
func TestDetectUnparseableFallsBackToGin(t *testing.T) {
	dir := writeTree(t, map[string]string{"broken.go": "package !!!not go"})
	if got := detect(dir); got != "gin" {
		t.Errorf("detect = %q, want %q for an unparseable dir", got, "gin")
	}
}

func TestDetectMissingDirFallsBackToGin(t *testing.T) {
	if got := detect(filepath.Join(t.TempDir(), "does-not-exist")); got != "gin" {
		t.Errorf("detect = %q, want %q for a missing dir", got, "gin")
	}
}

func TestAdapterFor(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	// An explicit Adapter wins over detection; an unknown name falls back to gin
	// rather than erroring, which is what the switch's default arm promises.
	for _, name := range []string{"chi", "echo", "stdlib", "gin", "", "nonsense"} {
		if a := adapterFor(Config{Dir: dir, Adapter: name}); a == nil {
			t.Errorf("adapterFor(%q) returned nil adapter", name)
		}
	}
}

func TestGenerate(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	doc, err := Generate(Config{Dir: dir, Title: "Widgets", Version: "1.2.3"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if doc.Info.Title != "Widgets" || doc.Info.Version != "1.2.3" {
		t.Errorf("info = %+v, want title/version passed through", doc.Info)
	}
	if _, ok := doc.Paths["/widgets"]; !ok {
		t.Errorf("path /widgets missing; got paths %v", keysOf(doc.Paths))
	}
}

func TestGenerateMissingDirErrors(t *testing.T) {
	_, err := Generate(Config{Dir: filepath.Join(t.TempDir(), "nope")})
	if err == nil {
		t.Error("Generate on a missing dir returned no error")
	}
}

const protoSrc = `syntax = "proto3";
package demo.v1;

message Ping { string msg = 1; }
message Pong { string msg = 1; }

service Echo {
  rpc Say(Ping) returns (Pong);
}
`

func TestGenerateGrpc(t *testing.T) {
	dir := writeTree(t, map[string]string{"api/echo.proto": protoSrc})
	doc, err := GenerateGrpc(Config{Dir: dir, ProtoDir: filepath.Join(dir, "api")})
	if err != nil {
		t.Fatalf("GenerateGrpc: %v", err)
	}
	if len(doc.Services) != 1 || doc.Services[0].Name != "Echo" {
		t.Fatalf("services = %+v, want one service named Echo", doc.Services)
	}
}

// With ProtoDir unset the scan falls back to Dir.
func TestGenerateGrpcDefaultsToDir(t *testing.T) {
	dir := writeTree(t, map[string]string{"echo.proto": protoSrc})
	doc, err := GenerateGrpc(Config{Dir: dir})
	if err != nil {
		t.Fatalf("GenerateGrpc: %v", err)
	}
	if len(doc.Services) != 1 {
		t.Errorf("services = %d, want 1 when ProtoDir defaults to Dir", len(doc.Services))
	}
}

func TestGenerateGrpcNoProtosIsEmptyNotError(t *testing.T) {
	doc, err := GenerateGrpc(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("GenerateGrpc on an empty dir: %v", err)
	}
	if len(doc.Services) != 0 {
		t.Errorf("services = %d, want 0", len(doc.Services))
	}
}

const sdlSrc = `type Thing {
  id: ID!
  name: String!
}

type Query {
  thing(id: ID!): Thing
}
`

func TestGenerateGraphql(t *testing.T) {
	dir := writeTree(t, map[string]string{"sdl/schema.graphql": sdlSrc})
	doc, err := GenerateGraphql(Config{Dir: dir, GraphqlDir: filepath.Join(dir, "sdl")})
	if err != nil {
		t.Fatalf("GenerateGraphql: %v", err)
	}
	if len(doc.Queries) != 1 || doc.Queries[0].Name != "thing" {
		t.Fatalf("queries = %+v, want one query named thing", doc.Queries)
	}
}

func TestGenerateGraphqlDefaultsToDir(t *testing.T) {
	dir := writeTree(t, map[string]string{"schema.graphql": sdlSrc})
	doc, err := GenerateGraphql(Config{Dir: dir})
	if err != nil {
		t.Fatalf("GenerateGraphql: %v", err)
	}
	if len(doc.Queries) != 1 {
		t.Errorf("queries = %d, want 1 when GraphqlDir defaults to Dir", len(doc.Queries))
	}
}

func TestGenerateGraphqlNoSchemaIsEmptyNotError(t *testing.T) {
	doc, err := GenerateGraphql(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("GenerateGraphql on an empty dir: %v", err)
	}
	if len(doc.Queries) != 0 {
		t.Errorf("queries = %d, want 0", len(doc.Queries))
	}
}

// ---- Handler ----

func handlerFor(t *testing.T) http.Handler {
	t.Helper()
	dir := writeTree(t, map[string]string{
		"main.go":        ginSrc,
		"api/echo.proto": protoSrc,
		"sdl/schema.gql": "",
		"sdl/s.graphql":  sdlSrc,
	})
	return Handler(Config{
		Dir:        dir,
		ProtoDir:   filepath.Join(dir, "api"),
		GraphqlDir: filepath.Join(dir, "sdl"),
		Title:      "T",
		Version:    "1",
	})
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestHandlerServesConsole(t *testing.T) {
	w := get(t, handlerFor(t), "/docs/")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(w.Body.String(), "<html") {
		t.Error("body does not look like the console page")
	}
}

func TestHandlerServesOpenAPI(t *testing.T) {
	w := get(t, handlerFor(t), "/docs/openapi.json")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if _, ok := doc.Paths["/widgets"]; !ok {
		t.Errorf("openapi.json missing /widgets; got %v", keysOf(doc.Paths))
	}
}

func TestHandlerServesGrpcAndGraphql(t *testing.T) {
	h := handlerFor(t)
	for _, path := range []string{"/docs/grpc.json", "/docs/graphql.json"} {
		w := get(t, h, path)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, w.Code)
		}
		if !json.Valid(w.Body.Bytes()) {
			t.Errorf("%s: body is not valid JSON", path)
		}
	}
}

// With nothing to document these must still answer with an empty document
// rather than a 404 or a 500 — the console fetches them unconditionally.
func TestHandlerEmptyGrpcAndGraphqlStillJSON(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir})
	for _, path := range []string{"/docs/grpc.json", "/docs/graphql.json"} {
		w := get(t, h, path)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, w.Code)
		}
		if !json.Valid(w.Body.Bytes()) {
			t.Errorf("%s: body is not valid JSON", path)
		}
	}
}

func TestHandlerGrpcInvokeRejectsGET(t *testing.T) {
	w := get(t, handlerFor(t), "/docs/grpc/invoke")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandlerGrpcInvokeRejectsBadJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/docs/grpc/invoke", strings.NewReader("{not json"))
	handlerFor(t).ServeHTTP(w, req)

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body["error"] == "" {
		t.Error("malformed body did not produce an error message")
	}
}

// A target that refuses connections must surface as a gateway error, not a
// panic or a hang.
func TestHandlerGrpcInvokeUnreachableTarget(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/docs/grpc/invoke",
		strings.NewReader(`{"target":"127.0.0.1:1","symbol":"demo.v1.Echo/Say","data":"{}"}`))
	handlerFor(t).ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body["error"] == "" {
		t.Error("unreachable target did not produce an error message")
	}
}

// A scan failure must reach the caller as a 500 rather than an empty page.
func TestHandlerReportsScanFailure(t *testing.T) {
	h := Handler(Config{Dir: filepath.Join(t.TempDir(), "missing")})
	w := get(t, h, "/docs/openapi.json")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// ---- generated-code fallbacks ----

// With no .proto in the tree the scan must fall back to generated *.pb.go
// stubs, so projects that ship only generated code are still documented.
func TestGenerateGrpcFallsBackToGeneratedStubs(t *testing.T) {
	doc, err := GenerateGrpc(Config{Dir: ".", ProtoDir: "examples/shop/shoppb"})
	if err != nil {
		t.Fatalf("GenerateGrpc: %v", err)
	}
	if len(doc.Services) == 0 {
		t.Fatal("no services recovered from .pb.go stubs")
	}
	if doc.Package != "shop.v1" {
		t.Errorf("package = %q, want shop.v1", doc.Package)
	}
}

// Likewise, with no SDL present the gqlgen resolver interfaces are the source.
func TestGenerateGraphqlFallsBackToGqlgen(t *testing.T) {
	doc, err := GenerateGraphql(Config{Dir: ".", GraphqlDir: "internal/gqlgenx/testdata"})
	if err != nil {
		t.Fatalf("GenerateGraphql: %v", err)
	}
	if len(doc.Queries) == 0 && len(doc.Types) == 0 {
		t.Fatal("no queries or types recovered from gqlgen sources")
	}
}

// When the gRPC or GraphQL scan fails outright the console must still receive
// a well-formed empty document — it fetches both unconditionally on load.
func TestHandlerScanFailureStillServesEmptyDocs(t *testing.T) {
	h := Handler(Config{Dir: filepath.Join(t.TempDir(), "missing")})
	for _, path := range []string{"/docs/grpc.json", "/docs/graphql.json"} {
		w := get(t, h, path)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, w.Code)
		}
		if !json.Valid(w.Body.Bytes()) {
			t.Errorf("%s: body is not valid JSON", path)
		}
	}
}

// ---- successful gRPC invoke ----

type testUserServer struct {
	shoppb.UnimplementedUserServiceServer
}

func (s *testUserServer) GetUser(ctx context.Context, req *shoppb.GetUserRequest) (*shoppb.User, error) {
	return &shoppb.User{Id: req.Id, Name: "Ada", Email: "ada@example.com"}, nil
}

// startTestGRPC brings up a real gRPC server on an ephemeral port; the invoke
// path resolves descriptors from the .proto files, so no reflection is needed.
func startTestGRPC(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	shoppb.RegisterUserServiceServer(s, &testUserServer{})
	go s.Serve(lis)
	t.Cleanup(s.Stop)
	return lis.Addr().String()
}

// The whole point of the console's Execute button: a real call must come back
// as the decoded response body, not merely "no error".
func TestHandlerGrpcInvokeSucceeds(t *testing.T) {
	target := startTestGRPC(t)
	h := Handler(Config{Dir: "examples/shop", ProtoDir: "examples/shop/proto"})

	body := `{"target":"` + target + `","symbol":"shop.v1.UserService/GetUser","data":"{\"id\":7}"}`
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/docs/grpc/invoke", strings.NewReader(body)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v\nbody: %s", err, w.Body.String())
	}
	if got["name"] != "Ada" {
		t.Errorf("name = %v, want Ada (body: %s)", got["name"], w.Body.String())
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---- AccessKey ----

func keyedHandler(t *testing.T, key string) http.Handler {
	t.Helper()
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	return Handler(Config{Dir: dir, AccessKey: key})
}

// The default stays open: adding the field must not silently lock existing
// deployments out of their own console.
func TestNoAccessKeyServesEveryone(t *testing.T) {
	h := keyedHandler(t, "")
	for _, path := range []string{"/docs/", "/docs/openapi.json"} {
		if got := get(t, h, path).Code; got != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, got)
		}
	}
}

// Without the key the console must not even confirm it exists.
func TestAccessKeyBlocksUnauthenticated(t *testing.T) {
	h := keyedHandler(t, "s3cret")
	for _, path := range []string{"/docs/", "/docs/openapi.json", "/docs/grpc.json", "/docs/graphql.json"} {
		w := get(t, h, path)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, w.Code)
		}
		if strings.Contains(w.Body.String(), "<html") {
			t.Errorf("%s: served the console without a key", path)
		}
	}
}

func TestAccessKeyViaQuery(t *testing.T) {
	w := get(t, keyedHandler(t, "s3cret"), "/docs/?key=s3cret")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<html") {
		t.Error("did not serve the console")
	}
}

func TestAccessKeyViaHeader(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil)
	req.Header.Set("X-Specter-Key", "s3cret")
	keyedHandler(t, "s3cret").ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// A key from the query string is stored so the page's relative fetches work
// without appending it to every URL.
func TestAccessKeyQuerySetsCookie(t *testing.T) {
	w := get(t, keyedHandler(t, "s3cret"), "/docs/?key=s3cret")

	var c *http.Cookie
	for _, got := range w.Result().Cookies() {
		if got.Name == accessCookie {
			c = got
		}
	}
	if c == nil {
		t.Fatalf("no %s cookie set; got %v", accessCookie, w.Result().Cookies())
	}
	if !c.HttpOnly {
		t.Error("cookie is readable from JavaScript")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
}

func TestAccessKeyViaCookie(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil)
	req.AddCookie(&http.Cookie{Name: accessCookie, Value: "s3cret"})
	keyedHandler(t, "s3cret").ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// No cookie is issued when the console is ungated.
func TestNoCookieWithoutAccessKey(t *testing.T) {
	w := get(t, keyedHandler(t, ""), "/docs/?key=anything")
	if len(w.Result().Cookies()) != 0 {
		t.Errorf("cookies = %v, want none", w.Result().Cookies())
	}
}

func TestWrongKeyRejected(t *testing.T) {
	h := keyedHandler(t, "s3cret")

	if got := get(t, h, "/docs/?key=wrong").Code; got != http.StatusNotFound {
		t.Errorf("wrong query key: status = %d, want 404", got)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
	req.Header.Set("X-Specter-Key", "wrong")
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("wrong header key: status = %d, want 404", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/docs/", nil)
	req.AddCookie(&http.Cookie{Name: accessCookie, Value: "wrong"})
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("wrong cookie: status = %d, want 404", w.Code)
	}
}

// A prefix of the key must not pass; this is what constant-time comparison of
// full values buys over a naive startswith.
func TestPartialKeyRejected(t *testing.T) {
	h := keyedHandler(t, "s3cret")
	for _, guess := range []string{"s", "s3cre", "s3cretX", ""} {
		if got := get(t, h, "/docs/?key="+guess).Code; got != http.StatusNotFound {
			t.Errorf("key %q: status = %d, want 404", guess, got)
		}
	}
}

// The gate covers the invoke endpoint too — it is the one that can reach
// other services.
func TestAccessKeyGatesGrpcInvoke(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/docs/grpc/invoke",
		strings.NewReader(`{"target":"127.0.0.1:1","symbol":"a/B","data":"{}"}`))
	keyedHandler(t, "s3cret").ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAuthorizedUnit(t *testing.T) {
	req := func(mut func(*http.Request)) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/docs/", nil)
		if mut != nil {
			mut(r)
		}
		return r
	}
	if !authorized(req(nil), "") {
		t.Error("an empty key must allow everything")
	}
	if authorized(req(nil), "k") {
		t.Error("a bare request must not pass a gated console")
	}
	// An empty credential must never satisfy a configured key.
	if authorized(req(func(r *http.Request) { r.Header.Set("X-Specter-Key", "") }), "k") {
		t.Error("empty header passed")
	}
}

// ---- BasePath ----

func TestBasePathOrDefault(t *testing.T) {
	cases := map[string]string{
		"":                  DefaultBasePath,
		"   ":               DefaultBasePath,
		"/docs":             "/docs",
		"docs":              "/docs",
		"/docs/":            "/docs",
		"/docs///":          "/docs",
		"internal/api-docs": "/internal/api-docs",
		"/v2/console":       "/v2/console",
		// Mounting at the root would swallow every other route on the router.
		"/": DefaultBasePath,
	}
	for in, want := range cases {
		if got := (Config{BasePath: in}).BasePathOrDefault(); got != want {
			t.Errorf("BasePath %q -> %q, want %q", in, got, want)
		}
	}
}

// ---- Servers and Security ----

func TestServersDeclared(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	doc, err := Generate(Config{Dir: dir, Servers: []core.Server{
		{URL: "https://api.example.com", Description: "production"},
		{URL: "http://localhost:8080"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Servers) != 2 {
		t.Fatalf("servers = %+v, want 2", doc.Servers)
	}
	if doc.Servers[0].URL != "https://api.example.com" || doc.Servers[0].Description != "production" {
		t.Errorf("servers[0] = %+v", doc.Servers[0])
	}
}

// Servers are optional: an undeclared document must not carry an empty key,
// which some validators reject.
func TestNoServersOmitsKey(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	doc, err := Generate(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"servers"`) {
		t.Errorf("document carries an empty servers key: %s", b)
	}
	if strings.Contains(string(b), `"security"`) {
		t.Errorf("document carries an empty security key: %s", b)
	}
	if strings.Contains(string(b), `"securitySchemes"`) {
		t.Errorf("document carries an empty securitySchemes key: %s", b)
	}
}

func TestSecuritySchemesDeclared(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	doc, err := Generate(Config{Dir: dir, Security: map[string]core.SecurityScheme{
		"bearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
		"apiKeyAuth": {Type: "apiKey", Name: "X-API-Key", In: "header"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	schemes := doc.Components.SecuritySchemes
	if len(schemes) != 2 {
		t.Fatalf("schemes = %+v, want 2", schemes)
	}
	if b := schemes["bearerAuth"]; b == nil || b.Type != "http" || b.Scheme != "bearer" || b.BearerFormat != "JWT" {
		t.Errorf("bearerAuth = %+v", b)
	}
	if k := schemes["apiKeyAuth"]; k == nil || k.Type != "apiKey" || k.Name != "X-API-Key" || k.In != "header" {
		t.Errorf("apiKeyAuth = %+v", k)
	}

	// Each declared scheme becomes an alternative at the document level.
	if len(doc.Security) != 2 {
		t.Fatalf("security = %+v, want one requirement per scheme", doc.Security)
	}
	named := map[string]bool{}
	for _, req := range doc.Security {
		for name, scopes := range req {
			named[name] = true
			if scopes == nil {
				t.Errorf("%s: scopes is nil, want an empty list", name)
			}
		}
	}
	if !named["bearerAuth"] || !named["apiKeyAuth"] {
		t.Errorf("security = %+v, want both schemes", doc.Security)
	}
}

// The document is regenerated on every build, so map iteration order must not
// leak into the output.
func TestSecurityOrderIsStable(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	cfg := Config{Dir: dir, Security: map[string]core.SecurityScheme{
		"zeta":  {Type: "http", Scheme: "basic"},
		"alpha": {Type: "http", Scheme: "bearer"},
		"mid":   {Type: "apiKey", Name: "K", In: "query"},
	}}

	var first string
	for i := 0; i < 8; i++ {
		doc, err := Generate(cfg)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(doc.Security)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = string(b)
			continue
		}
		if string(b) != first {
			t.Fatalf("security order changed between runs:\n %s\n %s", first, b)
		}
	}
	// Sorted by name, so alpha comes first.
	if !strings.HasPrefix(first, `[{"alpha"`) {
		t.Errorf("security = %s, want alphabetical order", first)
	}
}

// The handler serves the same declarations, since that is what codegen reads.
func TestHandlerServesSecurity(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	h := Handler(Config{Dir: dir, Security: map[string]core.SecurityScheme{
		"bearerAuth": {Type: "http", Scheme: "bearer"},
	}})

	w := get(t, h, "/docs/openapi.json")
	var doc struct {
		Security   []map[string][]string `json:"security"`
		Components struct {
			SecuritySchemes map[string]map[string]any `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Components.SecuritySchemes["bearerAuth"] == nil {
		t.Errorf("securitySchemes missing from the served spec: %s", w.Body.String())
	}
	if len(doc.Security) != 1 {
		t.Errorf("security = %+v, want one entry", doc.Security)
	}
}

// The public API must be usable from outside the module, which cannot import
// internal/core. Every type a caller has to name or construct needs an
// exported alias — this fails to compile if one is missing.
func TestPublicTypesAreNameable(t *testing.T) {
	var (
		_ Server         = Server{URL: "https://example.com"}
		_ SecurityScheme = SecurityScheme{Type: "http", Scheme: "bearer"}
		_ *Document
		_ *GrpcDoc
		_ *GraphqlDoc
		_ *Schema
		_ *Operation
	)

	cfg := Config{
		Servers:  []Server{{URL: "https://example.com"}},
		Security: map[string]SecurityScheme{"bearerAuth": {Type: "http", Scheme: "bearer"}},
	}
	if len(cfg.Servers) != 1 || len(cfg.Security) != 1 {
		t.Fatal("config could not be built from the exported types")
	}

	// The returned document is the aliased type, so it can be held in a
	// variable declared with the public name.
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	cfg.Dir = dir
	var doc *Document
	doc, err := Generate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Components.SecuritySchemes["bearerAuth"] == nil {
		t.Error("scheme did not survive through the public API")
	}
}
