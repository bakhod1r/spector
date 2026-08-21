//go:build integration

// Package tests holds end-to-end integration tests that drive the compiled
// spector CLI the way a user would, rather than calling the library directly.
// They are gated behind the `integration` build tag so the default
// `go test ./...` stays fast and hermetic:
//
//	go test -tags integration ./tests/
//
// The unit tests live next to the code they cover; the browser suite lives in
// e2e/. This package is the seam in between: the binary, its flags, and the
// shape of what it writes to stdout.
package tests

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// runSpector runs `go run ../cmd/spector` with args and returns stdout. The
// working directory is this package's directory, so paths are relative to it.
func runSpector(t *testing.T, args ...string) []byte {
	t.Helper()
	full := append([]string{"run", "../cmd/spector"}, args...)
	cmd := exec.Command("go", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("spector %s: %v\nstderr:\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes()
}

// TestGenerateShopOpenAPI generates the example API's OpenAPI document and
// checks it is well-formed and contains a route the scanner must have resolved.
func TestGenerateShopOpenAPI(t *testing.T) {
	out := runSpector(t, "-dir", "../examples/shop", "-title", "Shop", "-version", "1.0.0")

	var doc struct {
		OpenAPI string                            `json:"openapi"`
		Info    struct{ Title, Version string }   `json:"info"`
		Paths   map[string]map[string]interface{} `json:"paths"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\nfirst 200 bytes: %s", err, firstN(out, 200))
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.") {
		t.Errorf("openapi version = %q, want a 3.x document", doc.OpenAPI)
	}
	if doc.Info.Title != "Shop" {
		t.Errorf("info.title = %q, want Shop (the -title flag)", doc.Info.Title)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("document has no paths; the scanner resolved nothing")
	}
}

func firstN(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
