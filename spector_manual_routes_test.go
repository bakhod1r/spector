package spector

import (
	"strconv"
	"strings"
	"testing"
)

// manualDir is a package whose route path cannot be resolved statically, so
// the scan emits a dynamic-route diagnostic and documents nothing for it.
const manualSrc = `package app

import "net/http"

func Register(mux *http.ServeMux, paths []string) {
	for _, p := range paths {
		mux.HandleFunc(p, h)
	}
	mux.HandleFunc("GET /health", h)
}

func h(w http.ResponseWriter, r *http.Request) {}
`

func writeManualDir(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{"main.go": manualSrc})
}

func TestManualRouteAddedAndMarked(t *testing.T) {
	dir := writeManualDir(t)
	doc, err := Generate(Config{Dir: dir, Adapter: "stdlib", Routes: []ManualRoute{{
		Method:    "GET",
		Path:      "/v1/reports/{id}",
		Summary:   "Report by id",
		Tags:      []string{"reports"},
		Responses: []string{"200", "404"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	op := doc.Paths["/v1/reports/{id}"]["get"]
	if op == nil {
		t.Fatalf("manual route missing; paths = %v", doc.Paths)
	}
	if !op.Manual {
		t.Error("manual route not marked x-spector-manual")
	}
	if op.Summary != "Report by id" {
		t.Errorf("summary = %q", op.Summary)
	}
	if op.Responses["200"] == nil || op.Responses["404"] == nil {
		t.Errorf("responses = %v, want 200 and 404", op.Responses)
	}
	if doc.Paths["/health"]["get"] == nil {
		t.Error("scanned route lost")
	}
}

func TestManualRouteDefaultsToOK(t *testing.T) {
	dir := writeManualDir(t)
	doc, err := Generate(Config{Dir: dir, Adapter: "stdlib", Routes: []ManualRoute{{Path: "/v1/x"}}})
	if err != nil {
		t.Fatal(err)
	}
	op := doc.Paths["/v1/x"]["get"]
	if op == nil {
		t.Fatal("route with no method defaulted away")
	}
	if op.Responses["200"] == nil {
		t.Errorf("responses = %v, want a default 200", op.Responses)
	}
}

func TestManualRouteNeverOverridesSource(t *testing.T) {
	dir := writeManualDir(t)
	doc, err := Generate(Config{Dir: dir, Adapter: "stdlib", Routes: []ManualRoute{{
		Method:  "GET",
		Path:    "/health",
		Summary: "hand written",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	op := doc.Paths["/health"]["get"]
	if op.Summary == "hand written" {
		t.Error("manual supplement overwrote the scanned operation")
	}
	if op.Manual {
		t.Error("scanned operation flagged manual")
	}
}

func TestManualRouteFillsSuppressesDiagnostic(t *testing.T) {
	dir := writeManualDir(t)
	base, err := Generate(Config{Dir: dir, Adapter: "stdlib"})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Diagnostics) != 1 {
		t.Fatalf("want 1 dynamic-route diagnostic, got %d: %v", len(base.Diagnostics), base.Diagnostics)
	}
	pos := base.Diagnostics[0].Pos

	doc, err := Generate(Config{Dir: dir, Adapter: "stdlib", Routes: []ManualRoute{{
		Method: "GET",
		Path:   "/v1/reports/{id}",
		Fills:  "main.go:" + strconv.Itoa(pos.Line),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Diagnostics) != 0 {
		t.Fatalf("diagnostic not suppressed: %v", doc.Diagnostics)
	}
}

func TestManualRouteFillsMismatchKeepsDiagnostic(t *testing.T) {
	dir := writeManualDir(t)
	doc, err := Generate(Config{Dir: dir, Adapter: "stdlib", Routes: []ManualRoute{{
		Method: "GET",
		Path:   "/v1/reports/{id}",
		Fills:  "main.go:9999",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v, want the unmatched one kept", doc.Diagnostics)
	}
}

func TestManualRouteInvalidPathIsAnError(t *testing.T) {
	dir := writeManualDir(t)
	_, err := Generate(Config{Dir: dir, Adapter: "stdlib", Routes: []ManualRoute{{Path: "v1/x"}}})
	if err == nil || !strings.Contains(err.Error(), "leading /") {
		t.Fatalf("err = %v, want a leading-slash complaint", err)
	}
}
