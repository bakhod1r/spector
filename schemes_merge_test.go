package spector

import (
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// Declaring one scheme in Config must not throw away the schemes middleware
// implied: operations still reference those names.
func TestApplyDeclaredKeepsInferredSchemes(t *testing.T) {
	doc := core.NewDocument("t", "1")
	route := core.Route{Middleware: []core.Middleware{{Scheme: "bearerAuth"}}}
	applyInferredSchemes(doc, []core.Route{route})
	applyDeclared(doc, Config{Security: map[string]SecurityScheme{
		"apiKeyAuth": {Type: "apiKey", In: "header", Name: "X-API-Key"},
	}})
	if doc.Components.SecuritySchemes["bearerAuth"] == nil {
		t.Fatal("inferred bearerAuth was dropped; operations referencing it now dangle")
	}
	if doc.Components.SecuritySchemes["apiKeyAuth"] == nil {
		t.Fatal("declared apiKeyAuth missing")
	}
}
