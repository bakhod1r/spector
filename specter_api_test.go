package specter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The package's exported wrappers are the API a library caller programs
// against — the CLI is one consumer of them, not the only one. Exercising them
// here also means a change that breaks one is caught where it lives, rather
// than three packages away in a CLI test.

func docFor(t *testing.T) *Document {
	t.Helper()
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	doc, err := Generate(Config{Dir: dir, Title: "T", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestExportersRenderTheDocument(t *testing.T) {
	doc := docFor(t)

	for name, render := range map[string]func(*Document) ([]byte, error){
		"postman":     ExportPostman,
		"postman-env": ExportPostmanEnvironment,
		"har":         ExportHAR,
		"asyncapi":    ExportAsyncAPI,
	} {
		data, err := render(doc)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !json.Valid(data) {
			t.Errorf("%s: not valid JSON: %s", name, data)
		}
	}

	if md := ExportMarkdown(doc); !strings.Contains(string(md), "/widgets") {
		t.Errorf("markdown missing the documented path:\n%s", md)
	}
}

func TestToV31(t *testing.T) {
	tree, err := ToV31(docFor(t))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := tree["openapi"].(string); !strings.HasPrefix(v, "3.1") {
		t.Errorf("openapi = %v, want a 3.1 version", tree["openapi"])
	}
}

func TestGenerateTestsAndCoverage(t *testing.T) {
	doc := docFor(t)

	src := GenerateTests(doc, TestgenOptions{Package: "apitest"})
	if !strings.Contains(string(src), "package apitest") {
		t.Errorf("generated tests missing the package clause:\n%s", src)
	}

	report := MeasureCoverage(doc)
	if report.Operations == 0 {
		t.Errorf("coverage report counted no operations: %+v", report)
	}
}

func TestGenerateContract(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	files, err := GenerateContract(Config{Dir: dir}, ContractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no contract files")
	}
}

// A scan that fails must surface as an error rather than an empty contract.
func TestGenerateContractReportsAScanFailure(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": "package app\nfunc ("})
	if _, err := GenerateContract(Config{Dir: dir}, ContractOptions{}); err == nil {
		t.Error("want an error for a tree that does not parse")
	}
}

func TestLoadDocumentJSONAndYAML(t *testing.T) {
	doc := docFor(t)
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	jsonPath := filepath.Join(dir, "openapi.json")
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadDocument(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Paths["/widgets"] == nil {
		t.Errorf("loaded document lost its paths: %+v", loaded.Paths)
	}

	// YAML is bridged through JSON, including the nested maps a YAML decoder
	// hands back with non-string keys.
	yamlPath := filepath.Join(dir, "openapi.yaml")
	// Nested maps and sequences exercise the walk that turns a YAML decoder's
	// non-string keys into something json.Marshal accepts.
	yamlSrc := "openapi: 3.0.3\ninfo:\n  title: T\n  version: \"1\"\npaths:\n  /widgets:\n    get:\n      parameters:\n        - name: limit\n          in: query\n          schema:\n            type: string\n      responses:\n        \"200\":\n          description: OK\n          content:\n            application/json:\n              schema:\n                type: array\n                items:\n                  type: object\n"
	if err := os.WriteFile(yamlPath, []byte(yamlSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	fromYAML, err := LoadDocument(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if fromYAML.Paths["/widgets"] == nil {
		t.Errorf("YAML document lost its paths: %+v", fromYAML.Paths)
	}

	if _, err := LoadDocument(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("want an error for a file that is not there")
	}
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDocument(broken); err == nil {
		t.Error("want an error for a file that is not a document")
	}
}

func TestGenerateSDKFromDocument(t *testing.T) {
	files, err := GenerateSDKFromDocument(docFor(t), SDKOptions{Lang: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no SDK files")
	}
	if _, err := GenerateSDKFromDocument(nil, SDKOptions{Lang: "ts"}); err == nil {
		t.Error("want an error for a nil document")
	}
}

func TestNewProxyAndMergeObserved(t *testing.T) {
	doc := docFor(t)

	p, err := NewProxy(doc, ProxyOptions{Target: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("nil proxy")
	}

	// An observed fragment describes a route the scan never resolved; merging
	// it is what fills that gap.
	dir := t.TempDir()
	fragment := filepath.Join(dir, "observed.json")
	observed := `{"openapi":"3.0.3","info":{"title":"T","version":"1"},"paths":{"/observed":{"get":{"responses":{"200":{"description":"OK"}}}}}}`
	if err := os.WriteFile(fragment, []byte(observed), 0o644); err != nil {
		t.Fatal(err)
	}
	fragmentDoc, err := LoadDocument(fragment)
	if err != nil {
		t.Fatal(err)
	}
	merged := MergeObserved(doc, fragmentDoc)
	if merged.Paths["/observed"] == nil {
		t.Errorf("merge dropped the observed route: %v", keysOf(merged.Paths))
	}
}

// sameOrigin decides whether a browser page may open the console's gRPC
// stream, so its edges are worth stating.
func TestSameOrigin(t *testing.T) {
	cases := []struct {
		origin, host string
		want         bool
	}{
		{"http://example.com", "example.com", true},
		{"https://example.com", "example.com", true},
		{"https://evil.com", "example.com", false},
		{"example.com", "example.com", false}, // no scheme: not an origin
		{"", "example.com", false},
	}
	for _, c := range cases {
		if got := sameOrigin(c.origin, c.host); got != c.want {
			t.Errorf("sameOrigin(%q, %q) = %v, want %v", c.origin, c.host, got, c.want)
		}
	}
}

// Evolve is the API-change report. Its three baselines are exclusive, and the
// exclusivity is the part a caller gets wrong.
func TestEvolveAgainstABaselineDirectory(t *testing.T) {
	base := writeTree(t, map[string]string{"main.go": ginSrc})
	next := writeTree(t, map[string]string{
		"main.go": strings.Replace(ginSrc, `r.POST("/widgets", func(c *gin.Context) {})`, "", 1),
	})

	changes, err := Evolve(Config{Dir: next}, EvolveOptions{BaselineDir: base})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Error("removing an operation produced no changes")
	}
}

func TestEvolveAgainstABaselineDocument(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	doc, err := Generate(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	baseline := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(baseline, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Compared against itself, nothing changed.
	changes, err := Evolve(Config{Dir: dir}, EvolveOptions{BaselineJSON: baseline})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %+v, want none against an identical baseline", changes)
	}
}

func TestEvolveRejectsAmbiguousBaselines(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})

	if _, err := Evolve(Config{Dir: dir}, EvolveOptions{}); err == nil {
		t.Error("want an error when no baseline is given")
	}
	_, err := Evolve(Config{Dir: dir}, EvolveOptions{BaselineDir: dir, BaselineJSON: "x.json"})
	if err == nil {
		t.Error("want an error when two baselines are given")
	}
	if _, err := Evolve(Config{Dir: dir}, EvolveOptions{BaselineJSON: "missing.json"}); err == nil {
		t.Error("want an error for a baseline document that is not there")
	}
}

// The gateway document is built from google.api.http annotations, which is a
// different scanner from the REST one.
func TestGenerateGateway(t *testing.T) {
	doc, err := GenerateGateway(Config{Dir: "internal/proto", ProtoDir: "internal/proto/testdata_gateway"})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Paths) == 0 {
		t.Errorf("no annotated methods became paths: %+v", doc)
	}
}
