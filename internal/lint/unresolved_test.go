package lint

import (
	"go/token"
	"strings"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// An unresolved registration is the routing problem with the largest
// consequence — the endpoint serves traffic and appears in no document — and
// it was the one -lint did not report. A CI job gating on the linter called a
// tree clean while half its routes were undocumented.
func TestUnresolvedRouteIsReported(t *testing.T) {
	diags := []core.Diagnostic{{
		Pos:    token.Position{Filename: "router.go", Line: 42},
		Kind:   "route",
		Reason: "non-literal expression",
	}}

	found, err := AnalyzeWith(t.TempDir(), nil, diags)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("findings = %v, want the unresolved route", found)
	}
	f := found[0]
	if f.Kind != UnresolvedRoute {
		t.Errorf("kind = %q, want %q", f.Kind, UnresolvedRoute)
	}
	if f.Source == nil || f.Source.File != "router.go" || f.Source.Line != 42 {
		t.Errorf("source = %+v, want router.go:42", f.Source)
	}
	if !strings.Contains(f.Message, "non-literal expression") {
		t.Errorf("message drops the reason: %q", f.Message)
	}
}

// Analyze keeps its old signature and its old answer, so a caller that has no
// diagnostics to offer is unaffected.
func TestAnalyzeWithoutDiagnostics(t *testing.T) {
	found, err := Analyze(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("findings = %v, want none", found)
	}
}
