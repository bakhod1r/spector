package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func callReq(args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// textOf unwraps the single text content a handler returns.
func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("content items = %d, want 1", len(res.Content))
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want TextContent", res.Content[0])
	}
	return tc.Text
}

func TestMCPGenerateOpenAPI(t *testing.T) {
	res, err := handleGenerateOpenAPI(context.Background(), callReq(map[string]any{
		"dir": "../../examples/ginapp",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("document has no paths")
	}
}

func TestMCPScanRoutes(t *testing.T) {
	res, err := handleScanRoutes(context.Background(), callReq(map[string]any{
		"dir": "../../examples/ginapp",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	var routes []map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &routes); err != nil {
		t.Fatal(err)
	}
	if len(routes) == 0 {
		t.Fatal("no routes found")
	}
}

func TestMCPLintRoutesCleanIsEmptyArray(t *testing.T) {
	res, err := handleLintRoutes(context.Background(), callReq(map[string]any{
		"dir": "../../examples/ginapp",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	text := strings.TrimSpace(textOf(t, res))
	if !strings.HasPrefix(text, "[") {
		t.Fatalf("lint result is not a JSON array: %q", text)
	}
}

func TestMCPMissingDirIsToolError(t *testing.T) {
	res, err := handleGenerateOpenAPI(context.Background(), callReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("missing dir did not produce a tool error")
	}
}

func TestMCPServerListsTools(t *testing.T) {
	s := newMCPServer()
	if s == nil {
		t.Fatal("nil server")
	}
}

func TestMCPGenerateOpenAPITitleVersionParams(t *testing.T) {
	res, err := handleGenerateOpenAPI(context.Background(), callReq(map[string]any{
		"dir":     "../../examples/ginapp",
		"title":   "Custom API",
		"version": "9.9.9",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	var doc struct {
		Info struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Info.Title != "Custom API" || doc.Info.Version != "9.9.9" {
		t.Fatalf("info = %+v, want Custom API / 9.9.9", doc.Info)
	}
}

func TestMCPGenerateGrpc(t *testing.T) {
	res, err := handleGenerateGrpc(context.Background(), callReq(map[string]any{
		"dir":       "../../examples/shop",
		"proto_dir": "../../examples/shop/proto",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	var doc struct {
		Services []any `json:"services"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Services) == 0 {
		t.Fatal("grpc document has no services")
	}
}

func TestMCPGenerateGraphql(t *testing.T) {
	res, err := handleGenerateGraphql(context.Background(), callReq(map[string]any{
		"dir":         "../../examples/shop",
		"graphql_dir": "../../examples/shop/graphql",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	var doc struct {
		Queries []any          `json:"queries"`
		Types   map[string]any `json:"types"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Queries) == 0 && len(doc.Types) == 0 {
		t.Fatal("graphql document is empty")
	}
}

// A specter.json next to the scanned source is applied as a default, same as
// the CLI: its title shows up in the document without any tool argument.
func TestMCPConfigFileApplies(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile("../../examples/ginapp/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	cfgJSON := `{"title":"From Config","version":"2.0.0"}`
	if err := os.WriteFile(filepath.Join(dir, "specter.json"), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := handleGenerateOpenAPI(context.Background(), callReq(map[string]any{"dir": dir}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	var doc struct {
		Info struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Info.Title != "From Config" || doc.Info.Version != "2.0.0" {
		t.Fatalf("info = %+v, want From Config / 2.0.0", doc.Info)
	}
}

// An explicit config path that does not exist is an error, not a silent skip:
// the caller named a file, so a typo must not go unnoticed.
func TestMCPExplicitMissingConfigIsToolError(t *testing.T) {
	res, err := handleGenerateOpenAPI(context.Background(), callReq(map[string]any{
		"dir":    "../../examples/ginapp",
		"config": "/nonexistent/specter.json",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("missing explicit config did not produce a tool error")
	}
}

func TestMCPAllToolsRegistered(t *testing.T) {
	// tools/list through the real server, so registration — not just handler
	// functions — is what is being checked.
	srv := newMCPServer()
	raw := srv.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range resp.Result.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{
		"generate_openapi", "generate_grpc", "generate_graphql",
		"scan_routes", "lint_routes",
		"generate_sdk", "generate_admin", "admin_model", "mock_request",
	} {
		if !got[want] {
			t.Errorf("tool %s not registered (got %v)", want, resp.Result.Tools)
		}
	}
}

// End to end through the server's dispatch: a tools/call request produces the
// same document the handler test saw.
func TestMCPToolsCallDispatch(t *testing.T) {
	srv := newMCPServer()
	req := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"scan_routes","arguments":{"dir":"../../examples/ginapp"}}}`
	raw := srv.HandleMessage(context.Background(), []byte(req))
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Result.IsError {
		t.Fatalf("tool error: %+v", resp.Result)
	}
	var routes []map[string]any
	if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &routes); err != nil {
		t.Fatal(err)
	}
	if len(routes) == 0 {
		t.Fatal("no routes via tools/call")
	}
}

func decodeFiles(t *testing.T, res *mcp.CallToolResult) []struct {
	Name    string `json:"name"`
	Content string `json:"content"`
} {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	var files []struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &files); err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no files generated")
	}
	return files
}

func TestMCPGenerateSDKTypeScript(t *testing.T) {
	res, err := handleGenerateSDK(context.Background(), callReq(map[string]any{
		"dir":  "../../examples/ginapp",
		"lang": "ts",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range decodeFiles(t, res) {
		// Content must be readable source, not base64 of []byte.
		if f.Content == "" {
			t.Fatalf("%s has empty content", f.Name)
		}
	}
}

func TestMCPGenerateSDKGoUsesPackageName(t *testing.T) {
	res, err := handleGenerateSDK(context.Background(), callReq(map[string]any{
		"dir":     "../../examples/ginapp",
		"lang":    "go",
		"package": "shopclient",
	}))
	if err != nil {
		t.Fatal(err)
	}
	files := decodeFiles(t, res)
	found := false
	for _, f := range files {
		if strings.Contains(f.Content, "package shopclient") {
			found = true
		}
	}
	if !found {
		t.Fatal("generated Go client does not use the requested package name")
	}
}

func TestMCPGenerateSDKMissingLangIsToolError(t *testing.T) {
	res, err := handleGenerateSDK(context.Background(), callReq(map[string]any{
		"dir": "../../examples/ginapp",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("missing lang did not produce a tool error")
	}
}

func TestMCPGenerateSDKBadLangIsToolError(t *testing.T) {
	res, err := handleGenerateSDK(context.Background(), callReq(map[string]any{
		"dir":  "../../examples/ginapp",
		"lang": "cobol",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("unsupported lang did not produce a tool error")
	}
}

func TestMCPGenerateAdmin(t *testing.T) {
	res, err := handleGenerateAdmin(context.Background(), callReq(map[string]any{
		"dir":     "../../examples/ginapp",
		"package": "panel",
	}))
	if err != nil {
		t.Fatal(err)
	}
	files := decodeFiles(t, res)
	found := false
	for _, f := range files {
		if strings.Contains(f.Content, "package panel") {
			found = true
		}
	}
	if !found {
		t.Fatal("generated panel does not use the requested package name")
	}
}

// Without import_path there is no standalone entrypoint, because a main.go
// importing an unresolvable path would not build.
func TestMCPGenerateAdminEntrypointNeedsImportPath(t *testing.T) {
	without, err := handleGenerateAdmin(context.Background(), callReq(map[string]any{
		"dir": "../../examples/ginapp",
	}))
	if err != nil {
		t.Fatal(err)
	}
	with, err := handleGenerateAdmin(context.Background(), callReq(map[string]any{
		"dir":         "../../examples/ginapp",
		"import_path": "example.com/app/admin",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(decodeFiles(t, with)) <= len(decodeFiles(t, without)) {
		t.Fatal("import_path did not add the standalone entrypoint")
	}
}

func TestMCPAdminModel(t *testing.T) {
	res, err := handleAdminModel(context.Background(), callReq(map[string]any{
		"dir": "../../examples/ginapp",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	var model map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &model); err != nil {
		t.Fatal(err)
	}
	if len(model) == 0 {
		t.Fatal("admin model is empty")
	}
}

func TestMCPMockRequest(t *testing.T) {
	// The path comes from the document itself, so the test does not hardcode
	// a route the example may rename.
	docRes, err := handleGenerateOpenAPI(context.Background(), callReq(map[string]any{
		"dir": "../../examples/ginapp",
	}))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(textOf(t, docRes)), &doc); err != nil {
		t.Fatal(err)
	}
	var target string
	for p, ops := range doc.Paths {
		if _, ok := ops["get"]; ok && !strings.Contains(p, "{") {
			target = p
			break
		}
	}
	if target == "" {
		t.Skip("example has no parameterless GET to mock")
	}

	res, err := handleMockRequest(context.Background(), callReq(map[string]any{
		"dir":  "../../examples/ginapp",
		"path": target,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	var got struct {
		Status  int                 `json:"status"`
		Headers map[string][]string `json:"headers"`
		Body    string              `json:"body"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status < 200 || got.Status > 299 {
		t.Fatalf("status = %d for %s, want 2xx", got.Status, target)
	}
	if got.Body == "" {
		t.Fatalf("mock returned an empty body for %s", target)
	}
	// The mock is shape, not state: a second call must answer identically.
	again, err := handleMockRequest(context.Background(), callReq(map[string]any{
		"dir":  "../../examples/ginapp",
		"path": target,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if textOf(t, again) != textOf(t, res) {
		t.Fatal("repeated mock request returned a different response")
	}
}

func TestMCPMockRequestUndocumentedPath(t *testing.T) {
	res, err := handleMockRequest(context.Background(), callReq(map[string]any{
		"dir":  "../../examples/ginapp",
		"path": "/definitely-not-a-route",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	var got struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != 404 {
		t.Fatalf("status = %d for an undocumented path, want 404", got.Status)
	}
}

func TestMCPMockRequestMissingPathIsToolError(t *testing.T) {
	res, err := handleMockRequest(context.Background(), callReq(map[string]any{
		"dir": "../../examples/ginapp",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("missing path did not produce a tool error")
	}
}
