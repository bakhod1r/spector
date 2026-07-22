package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/user/specter"
)

// runMCP serves specter's generators and linter as MCP tools over stdio, so an
// AI client can ask for a document or a lint report without shelling out and
// parsing CLI output. It blocks until stdin closes.
func runMCP(stderr io.Writer) int {
	s := newMCPServer()
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintln(stderr, "specter:", err)
		return 1
	}
	return 0
}

// newMCPServer is split from runMCP so tests can call tool handlers in-process
// without owning stdin.
func newMCPServer() *server.MCPServer {
	s := server.NewMCPServer("specter", "0.1.0", server.WithToolCapabilities(false))

	s.AddTool(mcp.NewTool("generate_openapi",
		mcp.WithDescription("Scan a Go project's routes and return its OpenAPI document as JSON."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("directory to scan")),
		mcp.WithString("title", mcp.Description("API title (defaults to directory name)")),
		mcp.WithString("version", mcp.Description("API version (default 0.1.0)")),
		mcp.WithString("adapter", mcp.Description("framework adapter (gin, chi, echo, fiber, gorillamux, stdlib); autodetected if empty")),
		mcp.WithString("config", mcp.Description("JSON config file (default: specter.json in dir, if present)")),
	), handleGenerateOpenAPI)

	s.AddTool(mcp.NewTool("generate_grpc",
		mcp.WithDescription("Scan a Go project's gRPC sources (.proto/.pb.go) and return the gRPC document as JSON."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("directory to scan")),
		mcp.WithString("proto_dir", mcp.Description("directory to scan for gRPC sources (defaults to dir)")),
		mcp.WithString("config", mcp.Description("JSON config file (default: specter.json in dir, if present)")),
	), handleGenerateGrpc)

	s.AddTool(mcp.NewTool("generate_graphql",
		mcp.WithDescription("Scan a Go project's GraphQL sources (.graphql/gqlgen) and return the GraphQL document as JSON."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("directory to scan")),
		mcp.WithString("graphql_dir", mcp.Description("directory to scan for GraphQL sources (defaults to dir)")),
		mcp.WithString("config", mcp.Description("JSON config file (default: specter.json in dir, if present)")),
	), handleGenerateGraphql)

	s.AddTool(mcp.NewTool("scan_routes",
		mcp.WithDescription("Scan a Go project and return the list of HTTP routes found, without building a document."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("directory to scan")),
		mcp.WithString("adapter", mcp.Description("framework adapter; autodetected if empty")),
		mcp.WithString("config", mcp.Description("JSON config file (default: specter.json in dir, if present)")),
	), handleScanRoutes)

	s.AddTool(mcp.NewTool("lint_routes",
		mcp.WithDescription("Scan a Go project's routes and report routing problems as JSON findings. An empty array means clean."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("directory to scan")),
		mcp.WithString("adapter", mcp.Description("framework adapter; autodetected if empty")),
		mcp.WithString("config", mcp.Description("JSON config file (default: specter.json in dir, if present)")),
	), handleLintRoutes)

	s.AddTool(mcp.NewTool("generate_sdk",
		mcp.WithDescription("Generate a typed client for the scanned API and return the source files as JSON: [{name, content}]. Nothing is written to disk — the caller decides where the files go."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("directory to scan")),
		mcp.WithString("lang", mcp.Required(), mcp.Description("client language: ts or go")),
		mcp.WithString("package", mcp.Description("package name for the generated Go client (default: client); ignored for ts")),
		mcp.WithString("base_url", mcp.Description("default server the client calls (default: the document's first server)")),
		mcp.WithString("adapter", mcp.Description("framework adapter; autodetected if empty")),
		mcp.WithString("config", mcp.Description("JSON config file (default: specter.json in dir, if present)")),
	), handleGenerateSDK)

	s.AddTool(mcp.NewTool("generate_admin",
		mcp.WithDescription("Generate a gin admin panel for the scanned API and return the source files as JSON: [{name, content}]. Nothing is written to disk."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("directory to scan")),
		mcp.WithString("package", mcp.Description("package name for the generated panel (default: admin)")),
		mcp.WithString("prefix", mcp.Description("path the panel is served under (default: /admin)")),
		mcp.WithString("base_url", mcp.Description("base URL the panel calls (default: the document's first server)")),
		mcp.WithString("import_path", mcp.Description("import path of the generated package; without it no standalone entrypoint is written")),
		mcp.WithString("adapter", mcp.Description("framework adapter; autodetected if empty")),
		mcp.WithString("config", mcp.Description("JSON config file (default: specter.json in dir, if present)")),
	), handleGenerateAdmin)

	s.AddTool(mcp.NewTool("admin_model",
		mcp.WithDescription("Report the resources an admin panel would contain, without generating any files. A dry run for generate_admin."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("directory to scan")),
		mcp.WithString("adapter", mcp.Description("framework adapter; autodetected if empty")),
		mcp.WithString("config", mcp.Description("JSON config file (default: specter.json in dir, if present)")),
	), handleAdminModel)

	s.AddTool(mcp.NewTool("mock_request",
		mcp.WithDescription("Call one documented path against specter's in-process mock and return the status, headers and body it answers with. No port is opened. The mock is shape, not state: it answers from the response schema, so repeated calls return the same body."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("directory to scan")),
		mcp.WithString("path", mcp.Required(), mcp.Description("request path, e.g. /users/1")),
		mcp.WithString("method", mcp.Description("HTTP method (default GET)")),
		mcp.WithString("body", mcp.Description("request body sent as application/json")),
		mcp.WithString("adapter", mcp.Description("framework adapter; autodetected if empty")),
		mcp.WithString("config", mcp.Description("JSON config file (default: specter.json in dir, if present)")),
	), handleMockRequest)

	return s
}

// genFile is how a generated file crosses the wire. The generators hand back
// []byte, which JSON would encode as base64 — useless to a client that wants
// to read or write source.
type genFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// mcpConfig builds a specter.Config from tool arguments, applying the same
// specter.json defaulting the CLI does. No flags are ever "passed" here, so an
// empty FlagSet makes applyConfigFile treat every file value as a default that
// applies — which is what a tool caller with no command line expects.
func mcpConfig(req mcp.CallToolRequest) (specter.Config, error) {
	dir, err := req.RequireString("dir")
	if err != nil {
		return specter.Config{}, err
	}
	cfg := specter.Config{
		Dir:        dir,
		Adapter:    req.GetString("adapter", ""),
		Title:      req.GetString("title", ""),
		Version:    req.GetString("version", "0.1.0"),
		ProtoDir:   req.GetString("proto_dir", ""),
		GraphqlDir: req.GetString("graphql_dir", ""),
	}
	fs := flag.NewFlagSet("specter-mcp", flag.ContinueOnError)
	if err := applyConfigFile(&cfg, fs, req.GetString("config", ""), dir); err != nil {
		return specter.Config{}, err
	}
	return cfg, nil
}

// jsonResult marshals v for the client; a marshal failure is a tool error, not
// a protocol one, so the client can see and report it.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func handleGenerateOpenAPI(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := mcpConfig(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	doc, err := specter.Generate(cfg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(doc)
}

func handleGenerateGrpc(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := mcpConfig(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	doc, err := specter.GenerateGrpc(cfg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(doc)
}

func handleGenerateGraphql(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := mcpConfig(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	doc, err := specter.GenerateGraphql(cfg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(doc)
}

func handleScanRoutes(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := mcpConfig(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	routes, err := specter.ScanRoutes(cfg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(routes)
}

func handleLintRoutes(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := mcpConfig(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	routes, err := specter.ScanRoutes(cfg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	findings, err := specter.Lint(cfg, routes)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	// nil marshals to "null"; a clean lint should read as an empty list.
	if findings == nil {
		findings = []specter.Finding{}
	}
	return jsonResult(findings)
}

func handleGenerateSDK(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := mcpConfig(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	lang, err := req.RequireString("lang")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	files, err := specter.GenerateSDK(cfg, specter.SDKOptions{
		Lang:    lang,
		Package: req.GetString("package", ""),
		BaseURL: req.GetString("base_url", ""),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]genFile, 0, len(files))
	for _, f := range files {
		out = append(out, genFile{Name: f.Name, Content: string(f.Data)})
	}
	return jsonResult(out)
}

func handleGenerateAdmin(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := mcpConfig(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	files, err := specter.GenerateAdmin(cfg, specter.AdminOptions{
		Package:    req.GetString("package", ""),
		Prefix:     req.GetString("prefix", ""),
		BaseURL:    req.GetString("base_url", ""),
		ImportPath: req.GetString("import_path", ""),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]genFile, 0, len(files))
	for _, f := range files {
		out = append(out, genFile{Name: f.Name, Content: string(f.Data)})
	}
	return jsonResult(out)
}

func handleAdminModel(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := mcpConfig(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	model, err := specter.AdminModel(cfg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(model)
}

// handleMockRequest runs the request through the mock handler directly rather
// than over a socket. A tool call is one request; binding a port to serve it
// would leak a listener the client never asked for and cannot close.
func handleMockRequest(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := mcpConfig(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	doc, err := specter.Generate(cfg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	method := strings.ToUpper(req.GetString("method", "GET"))
	var reqBody io.Reader
	if b := req.GetString("body", ""); b != "" {
		reqBody = strings.NewReader(b)
	}
	httpReq := httptest.NewRequest(method, path, reqBody)
	if reqBody != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	specter.MockHandler(doc, specter.MockOptions{}).ServeHTTP(rec, httpReq)

	res := rec.Result()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(struct {
		Status  int                 `json:"status"`
		Headers map[string][]string `json:"headers"`
		Body    string              `json:"body"`
	}{res.StatusCode, res.Header, string(body)})
}
