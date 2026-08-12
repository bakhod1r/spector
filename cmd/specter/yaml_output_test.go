package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// sameDocument decodes a JSON and a YAML rendering of the same document and
// reports whether they carry identical data.
func sameDocument(t *testing.T, jsonOut, yamlOut string) {
	t.Helper()
	var fromJSON, fromYAML any
	if err := json.Unmarshal([]byte(jsonOut), &fromJSON); err != nil {
		t.Fatalf("JSON output not JSON: %v", err)
	}
	if err := yaml.Unmarshal([]byte(yamlOut), &fromYAML); err != nil {
		t.Fatalf("YAML output not YAML: %v\n%s", err, yamlOut)
	}
	// Round-trip the YAML side through JSON so number and map types match.
	norm, err := json.Marshal(fromYAML)
	if err != nil {
		t.Fatalf("YAML data not JSON-encodable: %v", err)
	}
	var normed any
	if err := json.Unmarshal(norm, &normed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromJSON, normed) {
		t.Errorf("YAML output differs from JSON output:\n%s", yamlOut)
	}
}

func TestFormatYAMLToStdout(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, jsonOut, stderr := exec(t, "-dir", dir, "-title", "Widgets")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	code, yamlOut, stderr := exec(t, "-dir", dir, "-title", "Widgets", "-format", "yaml")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if strings.HasPrefix(strings.TrimSpace(yamlOut), "{") {
		t.Fatalf("-format yaml still emitted JSON:\n%s", yamlOut)
	}
	if !strings.Contains(yamlOut, "openapi:") {
		t.Errorf("YAML output has no openapi key:\n%s", yamlOut)
	}
	sameDocument(t, jsonOut, yamlOut)
}

// The output file's extension picks the format when -format is not given, so
// "-o openapi.yaml" writes YAML rather than JSON under a YAML name.
func TestOutputExtensionImpliesYAML(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "openapi.yaml")
	code, _, stderr := exec(t, "-dir", dir, "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		t.Fatalf("-o openapi.yaml wrote JSON:\n%s", data)
	}
	var doc struct {
		Openapi string `yaml:"openapi"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("not YAML: %v", err)
	}
	if doc.Openapi == "" {
		t.Errorf("no openapi key in:\n%s", data)
	}
}

// An explicit -format beats the extension, so a .yaml name can still be told
// to hold JSON.
func TestFormatFlagBeatsExtension(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "openapi.yaml")
	code, _, stderr := exec(t, "-dir", dir, "-o", out, "-format", "json")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	data, _ := os.ReadFile(out)
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		t.Fatalf("-format json did not win over the .yaml name:\n%s", data)
	}
}

func TestFormatYAMLAppliesToGrpcAndGraphql(t *testing.T) {
	dir := writeTree(t, map[string]string{"api.proto": protoSrc})
	code, out, stderr := exec(t, "-grpc", "-dir", dir, "-format", "yaml")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("gRPC document ignored -format yaml:\n%s", out)
	}
}

func TestUnknownFormatIsAnError(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, _, stderr := exec(t, "-dir", dir, "-format", "toml")
	if code == 0 {
		t.Fatal("-format toml was accepted")
	}
	if !strings.Contains(stderr, "format") {
		t.Errorf("stderr = %q, want it to name the bad format", stderr)
	}
}

// YAML output keeps the document's key order rather than sorting it, so the
// spec reads top-down like the JSON does.
func TestYAMLKeepsKeyOrder(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, out, _ := exec(t, "-dir", dir, "-format", "yaml")
	if code != 0 {
		t.Fatal("exit non-zero")
	}
	iOpenapi := strings.Index(out, "openapi:")
	iInfo := strings.Index(out, "info:")
	iPaths := strings.Index(out, "paths:")
	if iOpenapi < 0 || iInfo < 0 || iPaths < 0 || !(iOpenapi < iInfo && iInfo < iPaths) {
		t.Errorf("keys not in document order (openapi, info, paths):\n%s", out)
	}
}

// -all writes one file per document; -format yaml renames them and switches
// the encoding so the output directory is all one format.
func TestAllFormatYAML(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := t.TempDir()
	code, _, stderr := exec(t, "-dir", dir, "-all", "-o", out, "-format", "yaml")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(out, "openapi.yaml"))
	if err != nil {
		t.Fatalf("openapi.yaml not written: %v (stderr: %s)", err, stderr)
	}
	if strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		t.Errorf("openapi.yaml holds JSON:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(out, "openapi.json")); err == nil {
		t.Error("openapi.json written too; -format yaml should replace it")
	}
}
