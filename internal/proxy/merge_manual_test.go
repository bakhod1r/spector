package proxy

import (
	"testing"

	"github.com/user/specter/internal/core"
)

func manualDoc() *core.Document {
	d := core.NewDocument("t", "1")
	d.AddOperation("/v1/reports/{id}", "get", &core.Operation{
		Summary:   "Report",
		Responses: map[string]*core.Response{"200": {Description: "OK"}},
	})
	return d
}

func TestMergeManualAddsGapRouteMarkedManual(t *testing.T) {
	base := core.NewDocument("t", "1")
	base.AddOperation("/v1/health", "get", &core.Operation{Responses: map[string]*core.Response{"200": {Description: "OK"}}})

	out := MergeManual(base, manualDoc())
	op := out.Paths["/v1/reports/{id}"]["get"]
	if op == nil {
		t.Fatal("manual route not added")
	}
	if !op.Manual {
		t.Fatal("added manual route must be marked Manual")
	}
	if op.Observed {
		t.Fatal("manual route must not be marked Observed")
	}
	if len(base.Paths) != 1 {
		t.Fatal("base mutated")
	}
}

func TestMergeManualDoesNotOverwriteSource(t *testing.T) {
	base := core.NewDocument("t", "1")
	base.AddOperation("/v1/reports/{id}", "get", &core.Operation{
		Summary:   "From source",
		Responses: map[string]*core.Response{"200": {Description: "source"}},
	})
	m := manualDoc()
	m.Paths["/v1/reports/{id}"]["get"].Responses["404"] = &core.Response{Description: "missing"}

	out := MergeManual(base, m)
	op := out.Paths["/v1/reports/{id}"]["get"]
	if op.Summary != "From source" {
		t.Fatalf("summary = %q, want source to win", op.Summary)
	}
	if op.Responses["200"].Description != "source" {
		t.Fatal("existing response overwritten")
	}
	if op.Responses["404"] == nil {
		t.Fatal("new status code not added")
	}
	if op.Manual {
		t.Fatal("documented op must not be flagged Manual")
	}
}

func TestMergeObservedStillNotManual(t *testing.T) {
	base := core.NewDocument("t", "1")
	out := MergeObserved(base, manualDoc())
	op := out.Paths["/v1/reports/{id}"]["get"]
	if !op.Observed || op.Manual {
		t.Fatalf("Observed=%v Manual=%v, want true/false", op.Observed, op.Manual)
	}
}
