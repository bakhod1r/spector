package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- -all ----

func TestAllWritesEveryDocument(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"main.go":        ginSrc,
		"echo.proto":     protoSrc,
		"schema.graphql": sdlSrc,
	})
	out := t.TempDir()
	code, _, stderr := exec(t, "-all", "-dir", dir, "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	for _, name := range []string{"openapi.json", "grpc.json", "graphql.json"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("%s not written: %v", name, err)
		}
		if !json.Valid(data) {
			t.Errorf("%s is not valid JSON", name)
		}
	}
	if strings.Count(stderr, "wrote ") != 3 {
		t.Errorf("stderr = %q, want three write confirmations", stderr)
	}
}

// With no -o the current directory is the output directory.
func TestAllDefaultsOutputToCwd(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	work := t.TempDir()
	t.Chdir(work)
	code, _, stderr := exec(t, "-all", "-dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(work, "openapi.json")); err != nil {
		t.Errorf("openapi.json not written to cwd: %v", err)
	}
}

// A scan that finds nothing skips the artifact rather than writing an empty lie.
func TestAllSkipsEmptyArtifacts(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := t.TempDir()
	code, _, stderr := exec(t, "-all", "-dir", dir, "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "grpc.json skipped") || !strings.Contains(stderr, "graphql.json skipped") {
		t.Errorf("stderr = %q, want skip notices for grpc and graphql", stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "grpc.json")); !os.IsNotExist(err) {
		t.Error("grpc.json was written despite an empty scan")
	}
}

// A failing artifact is reported and skipped; the others are still written.
func TestAllReportsArtifactErrors(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	missing := filepath.Join(t.TempDir(), "nope")
	out := t.TempDir()
	code, _, stderr := exec(t, "-all", "-dir", dir, "-proto", missing, "-graphqlDir", missing, "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "grpc.json:") || !strings.Contains(stderr, "graphql.json:") {
		t.Errorf("stderr = %q, want per-artifact errors", stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "openapi.json")); err != nil {
		t.Errorf("openapi.json should still be written: %v", err)
	}
}

func TestAllNothingWrittenExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"readme.txt": "x"})
	code, _, stderr := exec(t, "-all", "-dir", dir, "-o", t.TempDir())
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "nothing was written") {
		t.Errorf("stderr = %q, want a nothing-was-written report", stderr)
	}
}

// -o pointing at an existing file cannot become a directory.
func TestAllMkdirFailureExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	blocked := filepath.Join(writeTree(t, map[string]string{"file.txt": "x"}), "file.txt")
	code, _, stderr := exec(t, "-all", "-dir", dir, "-o", blocked)
	if code != 1 {
		t.Errorf("exit = %d, want 1, stderr: %s", code, stderr)
	}
}

// A directory squatting on openapi.json makes the write fail.
func TestAllWriteFileFailureExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, "openapi.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := exec(t, "-all", "-dir", dir, "-o", out)
	if code != 1 {
		t.Errorf("exit = %d, want 1, stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "specter:") {
		t.Errorf("stderr = %q, want a prefixed error", stderr)
	}
}

// ---- -mock ----

// An unlistenable address makes ServeMock fail after the banners are printed.
func TestMockBadAddressExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, _, stderr := exec(t, "-dir", dir, "-mock", "not a valid address",
		"-mock-origin", "https://a.example, ,https://b.example")
	if code != 1 {
		t.Errorf("exit = %d, want 1, stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "mocking") {
		t.Errorf("stderr = %q, want the mocking banner", stderr)
	}
	if strings.Contains(stderr, "CORS open to any origin") {
		t.Errorf("stderr = %q, origins were given so the open-CORS notice is wrong", stderr)
	}
}

// With credentials and no origins the mock explains the echo-back behaviour.
func TestMockCredentialsWarning(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, _, stderr := exec(t, "-dir", dir, "-mock", "not a valid address", "-mock-credentials", "-mock-max-age", "60")
	if code != 1 {
		t.Errorf("exit = %d, want 1, stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "CORS open to any origin") {
		t.Errorf("stderr = %q, want the open-CORS notice", stderr)
	}
	if !strings.Contains(stderr, "origin is echoed back") {
		t.Errorf("stderr = %q, want the credentials explanation", stderr)
	}
}

func TestMockGenerateErrorExits1(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	code, _, stderr := exec(t, "-dir", missing, "-mock", ":0")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "specter:") {
		t.Errorf("stderr = %q, want a prefixed error", stderr)
	}
}

// ---- -lint ----

func TestLintCleanExit0(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, stdout, stderr := exec(t, "-lint", "-dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stdout: %s stderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "no routing problems found") {
		t.Errorf("stderr = %q, want the all-clear", stderr)
	}
}

const ginDupSrc = `package app

import "github.com/gin-gonic/gin"

func Register(r *gin.Engine) {
	r.GET("/widgets", func(c *gin.Context) {})
	r.GET("/widgets", func(c *gin.Context) {})
}
`

func TestLintFindingsExit1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginDupSrc})
	code, stdout, stderr := exec(t, "-lint", "-dir", dir)
	if code != 1 {
		t.Fatalf("exit = %d, want 1, stdout: %s stderr: %s", code, stdout, stderr)
	}
	if stdout == "" {
		t.Error("no findings on stdout")
	}
	if !strings.Contains(stderr, "problem(s) found") {
		t.Errorf("stderr = %q, want a problem count", stderr)
	}
}

func TestLintScanErrorExits1(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	code, _, stderr := exec(t, "-lint", "-dir", missing)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "specter:") {
		t.Errorf("stderr = %q, want a prefixed error", stderr)
	}
}

// ---- config adapter field ----

func TestConfigFileSetsAdapter(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"main.go":      ginSrc,
		"specter.json": `{"adapter": "gin"}`,
	})
	code, stdout, stderr := exec(t, "-dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "/widgets") {
		t.Errorf("stdout does not contain the route:\n%s", stdout)
	}
}

func TestProdFlagHidesSource(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})

	// Control: without -prod the document carries source references.
	code, stdout, stderr := exec(t, "-dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "x-specter-source") {
		t.Fatalf("dev document has no x-specter-source to hide:\n%s", stdout)
	}

	// -prod strips it.
	code, stdout, stderr = exec(t, "-dir", dir, "-prod")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if strings.Contains(stdout, "x-specter-source") {
		t.Errorf("-prod document still contains x-specter-source:\n%s", stdout)
	}
}

func TestMergeLearnedFillsAndEnriches(t *testing.T) {
	frag := `{
  "openapi":"3.0.3","info":{"title":"o","version":"1"},
  "paths":{
    "/widgets":{"get":{"responses":{"500":{"description":"Observed 2 time(s)"}}}},
    "/widgets/{id}":{"get":{"responses":{"200":{"description":"Observed 3 time(s)"}}}}
  },
  "components":{"schemas":{}}
}`
	dir := writeTree(t, map[string]string{
		"main.go":   ginSrc,
		"frag.json": frag,
	})
	code, stdout, stderr := exec(t, "-dir", dir, "-merge-learned", filepath.Join(dir, "frag.json"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "/widgets/{id}") {
		t.Errorf("merged doc missing the observed-only route:\n%s", stdout)
	}
	if !strings.Contains(stdout, "x-specter-observed") {
		t.Errorf("observed-only route is not marked x-specter-observed:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"500"`) {
		t.Errorf("documented route was not enriched with the observed 500:\n%s", stdout)
	}
}

func TestMergeLearnedBadFileExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"main.go":  ginSrc,
		"bad.json": `{ not json`,
	})
	// Missing file.
	if code, _, _ := exec(t, "-dir", dir, "-merge-learned", filepath.Join(dir, "nope.json")); code == 0 {
		t.Error("a missing -merge-learned file should exit non-zero")
	}
	// Malformed JSON — error names the file.
	code, _, stderr := exec(t, "-dir", dir, "-merge-learned", filepath.Join(dir, "bad.json"))
	if code == 0 {
		t.Error("a malformed -merge-learned file should exit non-zero")
	}
	if !strings.Contains(stderr, "bad.json") {
		t.Errorf("error does not name the fragment file:\n%s", stderr)
	}
}

func TestProxyMergeNeedsProxyLearn(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, _, stderr := exec(t, "-dir", dir, "-proxy", ":0", "-proxy-merge")
	if code == 0 {
		t.Fatal("-proxy-merge without -proxy-learn should exit non-zero")
	}
	if !strings.Contains(stderr, "proxy-learn") {
		t.Errorf("error does not mention -proxy-learn:\n%s", stderr)
	}
}

func TestConfigFileSetsProduction(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"main.go":      ginSrc,
		"specter.json": `{"production": true}`,
	})
	code, stdout, stderr := exec(t, "-dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if strings.Contains(stdout, "x-specter-source") {
		t.Errorf("production config file did not hide source:\n%s", stdout)
	}
}
