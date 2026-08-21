package synthx

import (
	"encoding/json"
	"net/mail"
	"strings"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// loginDoc mirrors the shape that produced lorem ipsum for every field: plain
// `type: string` properties whose only signal is the name.
func loginDoc() *core.Document {
	str := func() *core.Schema { return &core.Schema{Type: "string"} }
	doc := &core.Document{
		Paths: map[string]map[string]*core.Operation{
			"/auth/login": {
				"post": {
					RequestBody: &core.RequestBody{
						Required: true,
						Content: map[string]core.MediaType{
							"application/json": {Schema: &core.Schema{Ref: "#/components/schemas/LoginRequest"}},
						},
					},
					Responses: map[string]*core.Response{},
				},
			},
		},
	}
	doc.Components.Schemas = map[string]*core.Schema{
		"LoginRequest": {
			Type: "object",
			Properties: map[string]*core.Schema{
				"device_id":          str(),
				"email":              str(),
				"password":           str(),
				"phone_number":       str(),
				"timezone":           str(),
				"phone_country_code": {Type: "integer"},
			},
		},
	}
	return doc
}

func payload(t *testing.T, doc *core.Document, method, path string) map[string]any {
	t.Helper()
	raw, err := PayloadJSON(doc, method, path)
	if err != nil {
		t.Fatalf("PayloadJSON: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, raw)
	}
	return out
}

// lorem is what the old OpenAPI front door produced for every string: words
// drawn from the lorem word list. Its presence in a field is the bug.
var loremWords = map[string]bool{
	"lorem": true, "ipsum": true, "dolor": true, "sit": true, "amet": true,
	"consectetur": true, "adipiscing": true, "elit": true, "sed": true,
	"do": true, "eiusmod": true, "tempor": true,
}

func assertNotLorem(t *testing.T, field string, v any) {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("%s is %T, want a string", field, v)
	}
	if s == "" {
		t.Fatalf("%s is empty", field)
	}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if loremWords[strings.Trim(w, ".,")] {
			t.Errorf("%s = %q: still lorem ipsum", field, s)
			return
		}
	}
}

// A named string field generates data of its own kind, not prose. This is the
// whole point of the package: the name is the only signal these fields carry.
func TestNamedStringFieldsAreNotLorem(t *testing.T) {
	rec := payload(t, loginDoc(), "POST", "/auth/login")
	for _, f := range []string{"password", "phone_number", "timezone", "device_id"} {
		v, ok := rec[f]
		if !ok {
			t.Fatalf("payload has no %s: %v", f, rec)
		}
		assertNotLorem(t, f, v)
	}
}

// The email field stays what it always was — the format-less name alone is
// enough for it, and it must not regress while the others are fixed.
func TestEmailIsAnAddress(t *testing.T) {
	rec := payload(t, loginDoc(), "POST", "/auth/login")
	s, _ := rec["email"].(string)
	if _, err := mail.ParseAddress(s); err != nil {
		t.Errorf("email = %q: not an address (%v)", s, err)
	}
}

// A *_id string is an identifier, never a sentence: synth's synonym table has
// bare "id" but not the qualified names, so the package supplies the format.
func TestIdentifierFieldsAreIdentifiers(t *testing.T) {
	rec := payload(t, loginDoc(), "POST", "/auth/login")
	s, _ := rec["device_id"].(string)
	if strings.Contains(s, " ") {
		t.Errorf("device_id = %q: contains a space, so it is prose, not an id", s)
	}
}

// isIdentifier must not fire on a word that merely ends in the letters i-d.
func TestIsIdentifier(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"device_id", true}, {"deviceID", true}, {"id", true}, {"session-id", true},
		{"user_uuid", true}, {"valid", false}, {"paid", false}, {"password", false},
		{"", false},
	} {
		if got := isIdentifier(tc.name); got != tc.want {
			t.Errorf("isIdentifier(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A typed field keeps its JSON type: the name inference applies to strings, so
// an integer stays a number and does not turn into a country's name.
func TestIntegerStaysANumber(t *testing.T) {
	rec := payload(t, loginDoc(), "POST", "/auth/login")
	if _, ok := rec["phone_country_code"].(float64); !ok {
		t.Errorf("phone_country_code = %#v, want a number", rec["phone_country_code"])
	}
}

// An enum generates one of the documented values, and a numeric bound is
// respected — the document's constraints survive the JSON Schema round-trip.
func TestEnumAndBoundsSurvive(t *testing.T) {
	min, max := 10.0, 20.0
	doc := loginDoc()
	doc.Components.Schemas["LoginRequest"].Properties["channel"] = &core.Schema{
		Type: "string", Enum: []any{"sms", "push"},
	}
	doc.Components.Schemas["LoginRequest"].Properties["retries"] = &core.Schema{
		Type: "integer", Minimum: &min, Maximum: &max,
	}
	rec := payload(t, doc, "POST", "/auth/login")
	if ch, _ := rec["channel"].(string); ch != "sms" && ch != "push" {
		t.Errorf("channel = %q, want one of the enum values", ch)
	}
	if n, ok := rec["retries"].(float64); !ok || n < min || n > max {
		t.Errorf("retries = %#v, want %g..%g", rec["retries"], min, max)
	}
}

// An operation with no request body is the caller's 422, not a panic.
func TestErrors(t *testing.T) {
	doc := loginDoc()
	if _, err := PayloadJSON(doc, "GET", "/auth/login"); err == nil {
		t.Error("a method the path does not have should error")
	}
	if _, err := PayloadJSON(doc, "POST", "/nope"); err == nil {
		t.Error("an unknown path should error")
	}
	doc.Paths["/auth/login"]["post"].RequestBody = nil
	if _, err := PayloadJSON(doc, "POST", "/auth/login"); err == nil {
		t.Error("an operation with no request body should error")
	}
	if _, err := PayloadJSON(nil, "POST", "/auth/login"); err == nil {
		t.Error("a nil document should error")
	}
}

// A self-referential schema terminates instead of recursing forever.
func TestSelfReferenceTerminates(t *testing.T) {
	doc := loginDoc()
	doc.Components.Schemas["Node"] = &core.Schema{
		Type: "object",
		Properties: map[string]*core.Schema{
			"label": {Type: "string"},
			"child": {Ref: "#/components/schemas/Node"},
		},
	}
	doc.Components.Schemas["LoginRequest"].Properties["node"] = &core.Schema{Ref: "#/components/schemas/Node"}
	if _, err := PayloadJSON(doc, "POST", "/auth/login"); err != nil {
		t.Fatalf("PayloadJSON: %v", err)
	}
}
