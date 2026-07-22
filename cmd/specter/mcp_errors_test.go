package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

type mcpHandler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)

// Every tool reports a bad config path and a failed scan as a tool error the
// client can read, rather than as a protocol failure that kills the session.
func TestMCPHandlersReportConfigAndScanErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	handlers := map[string]mcpHandler{
		"generate_openapi": handleGenerateOpenAPI,
		"generate_grpc":    handleGenerateGrpc,
		"generate_graphql": handleGenerateGraphql,
		"scan_routes":      handleScanRoutes,
		"lint_routes":      handleLintRoutes,
		"generate_admin":   handleGenerateAdmin,
		"admin_model":      handleAdminModel,
	}
	for name, h := range handlers {
		t.Run(name+"/bad_config", func(t *testing.T) {
			res, err := h(context.Background(), callReq(map[string]any{
				"dir":    "../../examples/ginapp",
				"config": filepath.Join(missing, "specter.json"),
			}))
			if err != nil {
				t.Fatal(err)
			}
			if !res.IsError {
				t.Fatal("missing config file was not a tool error")
			}
		})
		t.Run(name+"/scan_failure", func(t *testing.T) {
			res, err := h(context.Background(), callReq(map[string]any{"dir": missing}))
			if err != nil {
				t.Fatal(err)
			}
			if !res.IsError {
				t.Fatal("unscannable directory was not a tool error")
			}
		})
	}
}

// generate_sdk and mock_request take a second required argument, so their
// error paths are checked with it supplied.
func TestMCPSDKAndMockReportConfigAndScanErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	res, err := handleGenerateSDK(context.Background(), callReq(map[string]any{
		"dir":    "../../examples/ginapp",
		"lang":   "ts",
		"config": filepath.Join(missing, "specter.json"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("generate_sdk: missing config was not a tool error")
	}

	res, err = handleMockRequest(context.Background(), callReq(map[string]any{
		"dir":    "../../examples/ginapp",
		"path":   "/x",
		"config": filepath.Join(missing, "specter.json"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("mock_request: missing config was not a tool error")
	}

	res, err = handleMockRequest(context.Background(), callReq(map[string]any{
		"dir":  missing,
		"path": "/x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("mock_request: unscannable directory was not a tool error")
	}
}

// A body turns the mocked call into a JSON request; the mock still answers.
func TestMCPMockRequestWithBody(t *testing.T) {
	res, err := handleMockRequest(context.Background(), callReq(map[string]any{
		"dir":    "../../examples/ginapp",
		"path":   "/users",
		"method": "post",
		"body":   `{"name":"ada"}`,
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
	if got.Status == 0 {
		t.Error("mock did not answer the request")
	}
}

// A value JSON cannot represent is a tool error, not a panic or a protocol
// error: the client sees what went wrong.
func TestJSONResultMarshalFailureIsToolError(t *testing.T) {
	res, err := jsonResult(make(chan int))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("unmarshalable value did not produce a tool error")
	}
	if !strings.Contains(textOf(t, res), "json") {
		t.Errorf("error text does not explain the failure: %q", textOf(t, res))
	}
}
