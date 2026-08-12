package proxy

import (
	"testing"

	"github.com/user/specter/internal/core"
)

func opWithStatuses(codes ...string) *core.Operation {
	op := core.NewOperation("")
	for _, c := range codes {
		op.Responses[c] = &core.Response{Description: c}
	}
	return op
}

// A base document with one documented route and a documented 200.
func baseDoc() *core.Document {
	d := core.NewDocument("API", "1.0.0")
	d.AddOperation("/users", "get", opWithStatuses("200"))
	return d
}

// Observed traffic: a new route the base lacks, plus a new status on a route it
// has.
func observedDoc() *core.Document {
	d := core.NewDocument("API (observed)", "1.0.0")
	d.AddOperation("/users/{id}", "get", opWithStatuses("200", "404")) // gap
	d.AddOperation("/users", "get", opWithStatuses("200", "500"))      // enrich
	return d
}

func TestMergeObservedFillsGap(t *testing.T) {
	out := MergeObserved(baseDoc(), observedDoc())
	op := out.Paths["/users/{id}"]["get"]
	if op == nil {
		t.Fatal("observed-only route was not added")
	}
	if !op.Observed {
		t.Error("added route is not marked Observed")
	}
}

func TestMergeObservedEnrichesStatuses(t *testing.T) {
	out := MergeObserved(baseDoc(), observedDoc())
	op := out.Paths["/users"]["get"]
	if op == nil {
		t.Fatal("documented route disappeared")
	}
	if _, ok := op.Responses["500"]; !ok {
		t.Error("observed status 500 was not appended to the documented op")
	}
	if op.Observed {
		t.Error("documented op wrongly marked Observed (only wholly-new ops are)")
	}
}

func TestMergeObservedSourceResponseWins(t *testing.T) {
	base := baseDoc()
	base.Paths["/users"]["get"].Responses["200"].Description = "from source"
	obs := core.NewDocument("o", "1")
	obs.AddOperation("/users", "get", opWithStatuses("200")) // observed 200 differs
	obs.Paths["/users"]["get"].Responses["200"].Description = "observed"

	out := MergeObserved(base, obs)
	if got := out.Paths["/users"]["get"].Responses["200"].Description; got != "from source" {
		t.Errorf("source response was overwritten: got %q, want %q", got, "from source")
	}
}

func TestMergeObservedDoesNotMutateInputs(t *testing.T) {
	base, obs := baseDoc(), observedDoc()
	_ = MergeObserved(base, obs)
	if _, ok := base.Paths["/users/{id}"]; ok {
		t.Error("base was mutated: it gained the observed-only route")
	}
	if _, ok := base.Paths["/users"]["get"].Responses["500"]; ok {
		t.Error("base was mutated: a documented op gained an observed status")
	}
}

func TestMergeObservedNilObserved(t *testing.T) {
	out := MergeObserved(baseDoc(), nil)
	if out.Paths["/users"]["get"] == nil {
		t.Error("merging nil observed lost the base route")
	}
}

func TestMergeObservedNilBase(t *testing.T) {
	// A nil base must not panic: everything observed becomes an added op.
	out := MergeObserved(nil, observedDoc())
	if out == nil || out.Paths["/users/{id}"]["get"] == nil {
		t.Fatal("merging into a nil base lost the observed routes")
	}
	if !out.Paths["/users/{id}"]["get"].Observed {
		t.Error("route merged into a nil base is not marked Observed")
	}
}
