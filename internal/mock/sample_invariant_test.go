package mock

import (
	"testing"

	"github.com/user/specter/internal/core"
)

// The rule this package states for itself is that a sampled value satisfies the
// schema it came from: a mock answering with data its own document would reject
// is worse than no mock, because a client written against it passes locally and
// fails on the real API.
//
// Echoing a path parameter back is the one piece of realism available without
// state, but it cannot be allowed to break that rule. GET /users/abc against a
// document whose id is an integer must not answer {"id": "abc"}.
func TestEchoedParameterNeverViolatesItsSchema(t *testing.T) {
	doc := core.NewDocument("t", "1")
	schema := &core.Schema{
		Type: "object",
		Properties: map[string]*core.Schema{
			"id": {Type: "integer"},
		},
	}

	got, ok := Sample(doc, schema, map[string]string{"id": "abc"}).(map[string]any)
	if !ok {
		t.Fatalf("sample = %#v, want an object", got)
	}
	if _, isString := got["id"].(string); isString {
		t.Errorf("id = %#v, a string where the document says integer", got["id"])
	}
}

// The echo still has to happen when it can: GET /users/42 answering with some
// other id is the confusion the echo exists to prevent.
func TestNumericParameterIsStillEchoed(t *testing.T) {
	doc := core.NewDocument("t", "1")
	schema := &core.Schema{
		Type:       "object",
		Properties: map[string]*core.Schema{"id": {Type: "integer"}},
	}

	got := Sample(doc, schema, map[string]string{"id": "42"}).(map[string]any)
	if got["id"] != 42 {
		t.Errorf("id = %#v, want the requested 42", got["id"])
	}
}

// A number or boolean in the path is as echoable as an integer, and answering
// with the string "1.5" where the document says number is the same violation.
func TestNumberAndBooleanParametersAreTyped(t *testing.T) {
	doc := core.NewDocument("t", "1")
	schema := &core.Schema{
		Type: "object",
		Properties: map[string]*core.Schema{
			"price":  {Type: "number"},
			"active": {Type: "boolean"},
		},
	}

	got := Sample(doc, schema, map[string]string{"price": "1.5", "active": "true"}).(map[string]any)
	if got["price"] != 1.5 {
		t.Errorf("price = %#v, want the requested 1.5 as a number", got["price"])
	}
	if got["active"] != true {
		t.Errorf("active = %#v, want the requested true as a boolean", got["active"])
	}
}
