package mock

import (
	"strings"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

func str() *core.Schema { return &core.Schema{Type: "string"} }

// An unformatted string used to come back as the literal "string" whatever it
// was called, so a token body said nothing about what it held. The name is the
// only signal such a field has, and it has to be used.
func TestNamedStringsBeatThePlaceholder(t *testing.T) {
	schema := &core.Schema{Type: "object", Properties: map[string]*core.Schema{
		"access_token":  str(),
		"refresh_token": str(),
		"session_id":    str(),
		"user_id":       str(),
		"timezone":      str(),
	}}
	got := Sample(core.NewDocument("t", "1"), schema, nil).(map[string]any)
	for name, v := range got {
		if v == "string" {
			t.Errorf("%s is still the %q placeholder", name, "string")
		}
		if s, ok := v.(string); !ok || s == "" {
			t.Errorf("%s = %#v, want a non-empty string", name, v)
		}
	}
}

// A name synth does not recognise keeps the placeholder: inventing prose for a
// field nothing is known about would be worse than saying "string".
func TestUnknownNameKeepsThePlaceholder(t *testing.T) {
	schema := &core.Schema{Type: "object", Properties: map[string]*core.Schema{"a": str()}}
	got := Sample(core.NewDocument("t", "1"), schema, nil).(map[string]any)
	if got["a"] != "string" {
		t.Errorf("a = %#v, want the plain placeholder", got["a"])
	}
}

// Exports and generated contract tests render Sample output, so the same
// document has to mock to the same body every time.
func TestNamedStringsAreDeterministic(t *testing.T) {
	schema := &core.Schema{Type: "object", Properties: map[string]*core.Schema{"email": str()}}
	first := Sample(core.NewDocument("t", "1"), schema, nil).(map[string]any)
	second := Sample(core.NewDocument("t", "1"), schema, nil).(map[string]any)
	if first["email"] != second["email"] {
		t.Errorf("email = %v then %v: the mock body is not stable", first["email"], second["email"])
	}
}

// A declared format is the document stating the shape outright; the name never
// overrides it.
func TestFormatWinsOverTheName(t *testing.T) {
	schema := &core.Schema{Type: "object", Properties: map[string]*core.Schema{
		"email": {Type: "string", Format: "uuid"},
	}}
	got := Sample(core.NewDocument("t", "1"), schema, nil).(map[string]any)
	if got["email"] != formatSamples["uuid"] {
		t.Errorf("email = %v, want the uuid sample the format declares", got["email"])
	}
}

// The generated value still has to satisfy its own schema: a length bound the
// document declares rules out a value that breaks it, placeholder or not.
func TestLengthBoundsStillHold(t *testing.T) {
	max := 4
	schema := &core.Schema{Type: "object", Properties: map[string]*core.Schema{
		"email": {Type: "string", MaxLength: &max},
	}}
	got := Sample(core.NewDocument("t", "1"), schema, nil).(map[string]any)
	s, _ := got["email"].(string)
	if len(s) > max {
		t.Errorf("email = %q, longer than the declared maxLength %d", s, max)
	}
}

// Nested objects and arrays carry the name down: a token inside data is as
// named as one at the root.
func TestNamesReachNestedFields(t *testing.T) {
	schema := &core.Schema{Type: "object", Properties: map[string]*core.Schema{
		"data": {Type: "object", Properties: map[string]*core.Schema{
			"access_token": str(),
		}},
		"emails": {Type: "array", Items: str()},
	}}
	got := Sample(core.NewDocument("t", "1"), schema, nil).(map[string]any)
	data := got["data"].(map[string]any)
	if data["access_token"] == "string" {
		t.Error("a nested token kept the placeholder")
	}
	if arr, ok := got["emails"].([]any); ok && len(arr) > 0 {
		if s, _ := arr[0].(string); !strings.Contains(s, "@") {
			t.Errorf("emails[0] = %q, want an address from the array's own name", s)
		}
	}
}
