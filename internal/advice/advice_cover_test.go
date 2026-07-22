package advice

import (
	"testing"

	"github.com/user/specter/internal/core"
)

func TestNilOperationGetsNoAdvice(t *testing.T) {
	if got := reviewOperation("get", "/x", nil); got != nil {
		t.Errorf("= %v, want nil", rules(got))
	}
}

// An error code with a nil response body is skipped in both passes.
func TestNilErrorResponseIsSkipped(t *testing.T) {
	got := reviewOperation("get", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"404": nil,
	}))
	if hasRule(got, "rfc9457-content-type") || hasRule(got, "rfc9457-fields") {
		t.Errorf("= %v, want no body-shape advice for a nil response", rules(got))
	}
}

// An error with a body that is neither problem+json nor plain json is not
// second-guessed field by field.
func TestNonJSONErrorBodyIsSkippedInFieldCheck(t *testing.T) {
	got := reviewOperation("get", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"404": {
			Description: "err",
			Content: map[string]core.MediaType{
				"text/plain": {Schema: &core.Schema{Type: "string"}},
			},
		},
	}))
	if hasRule(got, "rfc9457-fields") || hasRule(got, "rfc9457-content-type") {
		t.Errorf("= %v, want no advice on a text/plain body", rules(got))
	}
}

// A schema with no properties has nothing to compare against the problem
// members.
func TestNilSchemaHasNoMissingFields(t *testing.T) {
	got := reviewOperation("get", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"404": {
			Description: "err",
			Content:     map[string]core.MediaType{problemJSON: {Schema: nil}},
		},
	}))
	if hasRule(got, "rfc9457-fields") {
		t.Errorf("= %v, want no field advice for a nil schema", rules(got))
	}
	if missing := missingProblemFields(&core.Schema{Type: "object"}); missing != nil {
		t.Errorf("missingProblemFields on nil Properties = %v, want nil", missing)
	}
}
