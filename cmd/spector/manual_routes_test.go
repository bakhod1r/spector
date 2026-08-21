package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// dynSrc registers one route the AST cannot resolve (a path from a slice) plus
// one it can, so the scan emits exactly one dynamic-route diagnostic.
const dynSrc = `package app

import "github.com/gin-gonic/gin"

func Register(r *gin.Engine, paths []string) {
	for _, p := range paths {
		r.GET(p, h)
	}
	r.GET("/health", h)
}

func h(c *gin.Context) {}
`

const routesConfigYAML = `adapter: gin
routes:
  - method: GET
    path: /v1/reports/{id}
    summary: Report by id
    tags: [reports]
    responses: ["200", "404"]
`

func TestConfigRoutesSupplementDocument(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": dynSrc, "spector.yaml": routesConfigYAML})
	code, stdout, stderr := exec(t, "-dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Summary   string          `json:"summary"`
			Manual    bool            `json:"x-spector-manual"`
			Responses map[string]any  `json:"responses"`
			Tags      []string        `json:"tags"`
			Source    json.RawMessage `json:"x-spector-source"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout)
	}
	op, ok := doc.Paths["/v1/reports/{id}"]["get"]
	if !ok {
		t.Fatalf("supplemented route missing; paths = %v", doc.Paths)
	}
	if !op.Manual {
		t.Error("supplemented route not marked x-spector-manual")
	}
	if op.Summary != "Report by id" {
		t.Errorf("summary = %q", op.Summary)
	}
	if len(op.Responses) != 2 {
		t.Errorf("responses = %v, want 200 and 404", op.Responses)
	}
	if _, ok := doc.Paths["/health"]["get"]; !ok {
		t.Error("scanned route lost")
	}
}

// A supplement that names the diagnostic it answers clears it, so a fully
// supplemented codebase passes -strict-routes.
func TestConfigRoutesFillsSatisfiesStrictRoutes(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": dynSrc})
	code, _, stderr := exec(t, "-dir", dir, "-strict-routes")
	if code == 0 {
		t.Fatalf("-strict-routes passed with an unresolved route; stderr: %s", stderr)
	}
	line := diagLine(t, stderr)

	dir2 := writeTree(t, map[string]string{
		"main.go":      dynSrc,
		"spector.yaml": "adapter: gin\nroutes:\n  - method: GET\n    path: /v1/reports/{id}\n    fills: main.go:" + line + "\n",
	})
	code, _, stderr = exec(t, "-dir", dir2, "-strict-routes")
	if code != 0 {
		t.Fatalf("-strict-routes still failed after the route was supplemented; stderr: %s", stderr)
	}
}

// diagLine pulls the line number out of the "file:line:col: ..." diagnostic
// the scan prints to stderr.
func diagLine(t *testing.T, stderr string) string {
	t.Helper()
	for _, l := range strings.Split(stderr, "\n") {
		i := strings.Index(l, "main.go:")
		if i < 0 {
			continue
		}
		rest := l[i+len("main.go:"):]
		line, _, ok := strings.Cut(rest, ":")
		if ok {
			return line
		}
	}
	t.Fatalf("no main.go diagnostic in stderr: %s", stderr)
	return ""
}

func TestConfigRoutesBadPathIsAnError(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"main.go":      dynSrc,
		"spector.yaml": "adapter: gin\nroutes:\n  - path: v1/x\n",
	})
	code, _, stderr := exec(t, "-dir", dir)
	if code == 0 {
		t.Fatal("a path without a leading / was accepted")
	}
	if !strings.Contains(stderr, "leading /") {
		t.Errorf("stderr = %q, want a leading-slash complaint", stderr)
	}
}
