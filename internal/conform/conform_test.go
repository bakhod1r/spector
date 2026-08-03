package conform

import (
	"encoding/json"
	"strings"
	"testing"
)

func parse(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func user() map[string]*Schema {
	return map[string]*Schema{
		"User": {
			Type:     "object",
			Required: []string{"id", "name"},
			Properties: map[string]*Schema{
				"id":     {Type: "integer"},
				"name":   {Type: "string"},
				"status": {Type: "string", Enum: []any{"active", "archived"}},
			},
		},
	}
}

func TestAConformingValueHasNothingToReport(t *testing.T) {
	got := Check(user(), &Schema{Ref: "#/components/schemas/User"},
		parse(t, `{"id": 1, "name": "ada", "status": "active"}`), "response")
	if len(got) != 0 {
		t.Errorf("problems = %v, want none", got)
	}
}

// The three findings worth having, each unambiguous evidence that the document
// and the service disagree.
func TestMissingRequiredPropertyIsReported(t *testing.T) {
	got := Check(user(), &Schema{Ref: "#/components/schemas/User"}, parse(t, `{"id": 1}`), "response")
	if len(got) != 1 || !strings.Contains(got[0], "response.name") {
		t.Errorf("problems = %v, want the missing name named", got)
	}
}

func TestWrongTypeIsReported(t *testing.T) {
	got := Check(user(), &Schema{Ref: "#/components/schemas/User"},
		parse(t, `{"id": "1", "name": "ada"}`), "response")
	if len(got) != 1 || !strings.Contains(got[0], "documented as a integer, got a string") {
		t.Errorf("problems = %v, want the type mismatch stated both ways", got)
	}
}

func TestValueOutsideAnEnumIsReported(t *testing.T) {
	got := Check(user(), &Schema{Ref: "#/components/schemas/User"},
		parse(t, `{"id": 1, "name": "ada", "status": "deleted"}`), "response")
	if len(got) != 1 || !strings.Contains(got[0], "deleted") {
		t.Errorf("problems = %v, want the bad enum value named", got)
	}
}

// An array is checked element by element, and the index is named — a report
// saying only "the list is wrong" sends the reader searching.
func TestArrayElementsAreCheckedByIndex(t *testing.T) {
	got := Check(user(), &Schema{Type: "array", Items: &Schema{Ref: "#/components/schemas/User"}},
		parse(t, `[{"id": 1, "name": "ada"}, {"id": 2}]`), "response")
	if len(got) != 1 || !strings.Contains(got[0], "response[1].name") {
		t.Errorf("problems = %v, want the second element's missing name", got)
	}
}

// allOf means the value satisfies every member, so every member is checked
// against the same object rather than one winning.
func TestAllOfIsCheckedInFull(t *testing.T) {
	components := map[string]*Schema{
		"Audited": {Type: "object", Required: []string{"createdAt"}},
	}
	s := &Schema{AllOf: []*Schema{
		{Ref: "#/components/schemas/Audited"},
		{Type: "object", Required: []string{"id"}},
	}}
	got := Check(components, s, parse(t, `{"id": 1}`), "response")
	if len(got) != 1 || !strings.Contains(got[0], "createdAt") {
		t.Errorf("problems = %v, want the allOf member's requirement enforced", got)
	}
}

// A schema saying nothing constrains nothing. Inventing a finding here would
// make the checker noisy exactly where the document is weakest.
func TestAnEmptySchemaConstrainsNothing(t *testing.T) {
	if got := Check(nil, &Schema{}, parse(t, `"anything"`), "response"); len(got) != 0 {
		t.Errorf("problems = %v, want none", got)
	}
}

// A document whose refs point at each other must not hang the checker.
func TestCircularRefsTerminate(t *testing.T) {
	components := map[string]*Schema{
		"A": {Ref: "#/components/schemas/B"},
		"B": {Ref: "#/components/schemas/A"},
	}
	Check(components, &Schema{Ref: "#/components/schemas/A"}, parse(t, `{}`), "response")
}

// A ref with no definition is a broken document, not a crash.
func TestUnresolvableRefIsSilent(t *testing.T) {
	if got := Check(nil, &Schema{Ref: "#/components/schemas/Missing"}, parse(t, `{}`), "r"); len(got) != 0 {
		t.Errorf("problems = %v, want none", got)
	}
}

// ---- Undocumented ----

// An extra field is a finding, but never a failure: nothing written against the
// document breaks because of a field it does not know about.
func TestExtraPropertiesAreReportedSeparately(t *testing.T) {
	value := parse(t, `{"id": 1, "name": "ada", "nickname": "a"}`)
	if got := Check(user(), &Schema{Ref: "#/components/schemas/User"}, value, "response"); len(got) != 0 {
		t.Errorf("an extra field was treated as a violation: %v", got)
	}
	got := Undocumented(user(), &Schema{Ref: "#/components/schemas/User"}, value, "response")
	if len(got) != 1 || !strings.Contains(got[0], "nickname") {
		t.Errorf("undocumented = %v, want nickname", got)
	}
}

// A hundred-element list with the same extra field is one finding, not a
// hundred.
func TestExtraFieldsInAListAreReportedOnce(t *testing.T) {
	items := `[{"id":1,"name":"a","x":1},{"id":2,"name":"b","x":2},{"id":3,"name":"c","x":3}]`
	got := Undocumented(user(), &Schema{Type: "array", Items: &Schema{Ref: "#/components/schemas/User"}},
		parse(t, items), "response")
	if len(got) != 1 {
		t.Errorf("undocumented = %v, want one finding for the whole list", got)
	}
}

// A schema that documents no properties is not claiming the object is empty.
func TestNothingDocumentedMeansNothingUndocumented(t *testing.T) {
	got := Undocumented(nil, &Schema{Type: "object"}, parse(t, `{"a": 1}`), "response")
	if len(got) != 0 {
		t.Errorf("undocumented = %v, want none", got)
	}
}

func TestKindOfNamesWhatArrived(t *testing.T) {
	cases := map[string]string{
		`null`: "null",
		`true`: "a boolean",
		`1`:    "a number",
		`"s"`:  "a string",
		`[]`:   "an array",
		`{}`:   "an object",
	}
	for input, want := range cases {
		if got := KindOf(parse(t, input)); got != want {
			t.Errorf("KindOf(%s) = %q, want %q", input, got, want)
		}
	}
}
