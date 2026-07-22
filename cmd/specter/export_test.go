package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -postman renders the same document the default mode emits, as a collection.
func TestPostmanToStdout(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, stdout, stderr := exec(t, "-dir", dir, "-postman")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	var col map[string]any
	if err := json.Unmarshal([]byte(stdout), &col); err != nil {
		t.Fatalf("collection is not JSON: %v", err)
	}
	if _, ok := col["item"]; !ok {
		t.Errorf("collection has no items: %s", stdout)
	}
	if !strings.HasSuffix(stdout, "\n") {
		t.Error("output does not end in a newline")
	}
}

// -o applies to the exports as well, and the write is reported.
func TestPostmanWritesToFile(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "collection.json")
	code, stdout, stderr := exec(t, "-dir", dir, "-postman", "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing when -o is given", stdout)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("collection not written: %v", err)
	}
	if !strings.Contains(stderr, "wrote") {
		t.Errorf("write not reported: %q", stderr)
	}
}

func TestMarkdownExport(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, stdout, stderr := exec(t, "-dir", dir, "-markdown")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "/widgets") {
		t.Errorf("markdown does not document the route: %q", stdout)
	}
}

// An export of an empty scan still succeeds; the warning names the directory.
func TestExportWarnsOnEmptyScan(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": "package app\n"})
	code, _, stderr := exec(t, "-dir", dir, "-markdown")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "no routes found") {
		t.Errorf("no empty warning: %q", stderr)
	}
}

func TestExportScanFailureExits1(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if code, _, _ := exec(t, "-dir", missing, "-postman"); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if code, _, _ := exec(t, "-dir", missing, "-markdown"); code != 1 {
		t.Errorf("markdown exit = %d, want 1", code)
	}
}

// A stdout that refuses writes is a failure, not a silent success.
func TestExportStdoutWriteFailureExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	var stderr bytes.Buffer
	if code := run([]string{"-dir", dir, "-markdown"}, failingWriter{}, &stderr); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}

func TestExportUnwritableOutputExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(dir, "no-such-dir", "collection.json")
	if code, _, stderr := exec(t, "-dir", dir, "-postman", "-o", out); code != 1 {
		t.Errorf("exit = %d, want 1 (%s)", code, stderr)
	}
}

// -gen-tests writes runnable Go source rather than a document.
func TestGenTestsWritesFile(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "apitest", "api_test.go")
	code, _, stderr := exec(t, "-dir", dir, "-gen-tests", out, "-test-package", "apitest")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "package apitest") {
		t.Errorf("generated file does not use the requested package: %s", data)
	}
	if !strings.Contains(stderr, "run with:") {
		t.Errorf("no run hint: %q", stderr)
	}
}

// A path go test would never run is worth saying out loud.
func TestGenTestsWarnsOnBadSuffix(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "api.go")
	code, _, stderr := exec(t, "-dir", dir, "-gen-tests", out)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "will not run it") {
		t.Errorf("no suffix warning: %q", stderr)
	}
}

func TestGenTestsEmptyScanWarns(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": "package app\n"})
	out := filepath.Join(t.TempDir(), "api_test.go")
	code, _, stderr := exec(t, "-dir", dir, "-gen-tests", out)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "no routes found") {
		t.Errorf("no empty warning: %q", stderr)
	}
}

func TestGenTestsScanFailureExits1(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	out := filepath.Join(t.TempDir(), "api_test.go")
	if code, _, _ := exec(t, "-dir", missing, "-gen-tests", out); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}

func TestGenTestsMkdirFailureExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	// A regular file where the parent directory must go.
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(blocker, "sub", "api_test.go")
	if code, _, stderr := exec(t, "-dir", dir, "-gen-tests", out); code != 1 {
		t.Errorf("exit = %d, want 1 (%s)", code, stderr)
	}
}

func TestGenTestsWriteFailureExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	outDir := t.TempDir()
	// The path is an existing directory, so the write cannot succeed while
	// MkdirAll on its parent still can.
	out := filepath.Join(outDir, "api_test.go")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := exec(t, "-dir", dir, "-gen-tests", out); code != 1 {
		t.Errorf("exit = %d, want 1 (%s)", code, stderr)
	}
}

// -coverage reports rather than emits, and says nothing on stdout that is not
// the report.
func TestCoverageReport(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, stdout, stderr := exec(t, "-dir", dir, "-coverage")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if stdout == "" {
		t.Error("coverage report is empty")
	}
}

// -coverage-min turns the report into a gate, so CI can fail on it.
func TestCoverageMinBelowThresholdExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, stdout, stderr := exec(t, "-dir", dir, "-coverage-min", "100")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (%s)", code, stdout)
	}
	if !strings.Contains(stderr, "below the required") {
		t.Errorf("threshold failure not explained: %q", stderr)
	}
}

func TestCoverageScanFailureExits1(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if code, _, _ := exec(t, "-dir", missing, "-coverage"); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}

// -openapi-version 3.1 is a conversion of the same document: the paths survive
// and only the version string changes.
func TestOpenAPIVersion31(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, stdout, stderr := exec(t, "-dir", dir, "-openapi-version", "3.1")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	var doc struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Errorf("openapi = %q, want 3.1.0", doc.OpenAPI)
	}
	if len(doc.Paths) == 0 {
		t.Error("3.1 conversion lost the paths")
	}
}

func TestOpenAPIVersionUnsupportedExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, _, stderr := exec(t, "-dir", dir, "-openapi-version", "2.0")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "unsupported -openapi-version") {
		t.Errorf("stderr = %q", stderr)
	}
}

// The empty spelling is the same as 3.0, not an error.
func TestOpenAPIVersionEmptyIs30(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, stdout, stderr := exec(t, "-dir", dir, "-openapi-version", "")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, `"3.0`) {
		t.Errorf("not a 3.0 document: %q", stdout)
	}
}
