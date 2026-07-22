package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// schemaFor scans a struct declaration and returns the schema for one of its
// properties, which is how every rule below is checked: through the real
// scanner rather than by calling the parser directly.
func schemaFor(t *testing.T, src string) *Schema {
	t.Helper()
	schemas := scanSrc(t, src)
	s := schemas["T"]
	if s == nil {
		t.Fatalf("no schema for T in:\n%s", src)
	}
	return s
}

func prop(t *testing.T, s *Schema, name string) *Schema {
	t.Helper()
	p := s.Properties[name]
	if p == nil {
		t.Fatalf("no property %q; have %v", name, keysOf(s.Properties))
	}
	return p
}

func keysOf(m map[string]*Schema) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func wrap(fields string) string {
	return "package p\n\ntype T struct {\n" + fields + "\n}\n"
}

// ---- required ----

// required belongs on the object, not on the property: that is where JSON
// Schema puts it.
func TestRequiredLandsOnTheParent(t *testing.T) {
	s := schemaFor(t, wrap("\tName string `json:\"name\" binding:\"required\"`\n\tNick string `json:\"nick\"`"))
	if !contains(s.Required, "name") {
		t.Errorf("required = %v, want it to contain name", s.Required)
	}
	if contains(s.Required, "nick") {
		t.Errorf("nick is not required but appears in %v", s.Required)
	}
}

func TestRequiredUsesTheJSONName(t *testing.T) {
	s := schemaFor(t, wrap("\tUserName string `json:\"user_name\" binding:\"required\"`"))
	if !contains(s.Required, "user_name") {
		t.Errorf("required = %v, want the json name", s.Required)
	}
}

// ---- numeric bounds ----

func TestGteAndLte(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tAge int `json:\"age\" binding:\"gte=18,lte=120\"`")), "age")
	if p.Minimum == nil || *p.Minimum != 18 {
		t.Errorf("minimum = %v, want 18", p.Minimum)
	}
	if p.Maximum == nil || *p.Maximum != 120 {
		t.Errorf("maximum = %v, want 120", p.Maximum)
	}
	if p.ExclusiveMinimum || p.ExclusiveMaximum {
		t.Error("gte/lte are inclusive")
	}
}

// gt/lt are the same bounds with the endpoint excluded, which JSON Schema
// expresses with a separate flag rather than a different keyword.
func TestGtAndLtAreExclusive(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tScore float64 `json:\"score\" binding:\"gt=0,lt=1\"`")), "score")
	if p.Minimum == nil || *p.Minimum != 0 || !p.ExclusiveMinimum {
		t.Errorf("minimum = %v exclusive=%v, want 0 exclusive", p.Minimum, p.ExclusiveMinimum)
	}
	if p.Maximum == nil || *p.Maximum != 1 || !p.ExclusiveMaximum {
		t.Errorf("maximum = %v exclusive=%v, want 1 exclusive", p.Maximum, p.ExclusiveMaximum)
	}
}

// A zero bound has to survive. This is why the fields are pointers: `gte=0`
// says negatives are rejected, which is not the same as saying nothing.
func TestZeroBoundIsNotDroppedAsEmpty(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tN int `json:\"n\" binding:\"gte=0\"`")), "n")
	if p.Minimum == nil {
		t.Fatal("minimum was dropped; gte=0 is a real constraint")
	}
	if *p.Minimum != 0 {
		t.Errorf("minimum = %v, want 0", *p.Minimum)
	}
	// And it has to reach the JSON, not be omitted as a zero value.
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"minimum":0`) {
		t.Errorf("serialised as %s, want minimum:0", b)
	}
}

// ---- min/max, which mean three different things ----

func TestMinMaxOnNumberIsAValueBound(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tAge int `json:\"age\" binding:\"min=1,max=9\"`")), "age")
	if p.Minimum == nil || *p.Minimum != 1 || p.Maximum == nil || *p.Maximum != 9 {
		t.Errorf("min/max = %v/%v, want value bounds", p.Minimum, p.Maximum)
	}
	if p.MinLength != nil || p.MinItems != nil {
		t.Error("a number got a length or item bound")
	}
}

func TestMinMaxOnStringIsALength(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tName string `json:\"name\" binding:\"min=3,max=50\"`")), "name")
	if p.MinLength == nil || *p.MinLength != 3 || p.MaxLength == nil || *p.MaxLength != 50 {
		t.Errorf("minLength/maxLength = %v/%v, want 3/50", p.MinLength, p.MaxLength)
	}
	if p.Minimum != nil || p.MinItems != nil {
		t.Error("a string got a value or item bound")
	}
}

func TestMinMaxOnSliceIsAnItemCount(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tTags []string `json:\"tags\" binding:\"min=1,max=5\"`")), "tags")
	if p.MinItems == nil || *p.MinItems != 1 || p.MaxItems == nil || *p.MaxItems != 5 {
		t.Errorf("minItems/maxItems = %v/%v, want 1/5", p.MinItems, p.MaxItems)
	}
	if p.MinLength != nil || p.Minimum != nil {
		t.Error("an array got a length or value bound")
	}
}

// ---- len ----

func TestLenOnString(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tCode string `json:\"code\" binding:\"len=4\"`")), "code")
	if p.MinLength == nil || p.MaxLength == nil || *p.MinLength != 4 || *p.MaxLength != 4 {
		t.Errorf("= %v/%v, want both 4", p.MinLength, p.MaxLength)
	}
}

func TestLenOnSlice(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tPair []int `json:\"pair\" binding:\"len=2\"`")), "pair")
	if p.MinItems == nil || p.MaxItems == nil || *p.MinItems != 2 || *p.MaxItems != 2 {
		t.Errorf("= %v/%v, want both 2", p.MinItems, p.MaxItems)
	}
}

// ---- oneof ----

func TestOneOfOnStringBecomesAnEnum(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tS string `json:\"s\" binding:\"oneof=draft sent paid\"`")), "s")
	want := []any{"draft", "sent", "paid"}
	if len(p.Enum) != len(want) {
		t.Fatalf("enum = %v, want %v", p.Enum, want)
	}
	for i := range want {
		if p.Enum[i] != want[i] {
			t.Errorf("enum[%d] = %v, want %v", i, p.Enum[i], want[i])
		}
	}
}

// The enum has to carry the field's type, or the document claims an integer
// field accepts the string "1".
func TestOneOfOnIntegerIsTyped(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tN int `json:\"n\" binding:\"oneof=1 2 3\"`")), "n")
	if len(p.Enum) != 3 {
		t.Fatalf("enum = %v", p.Enum)
	}
	if _, ok := p.Enum[0].(int); !ok {
		t.Errorf("enum[0] is %T, want int", p.Enum[0])
	}
}

// A non-numeric value on a numeric field means the tag and the type disagree.
// Guessing which is right would put a false claim in the document.
func TestOneOfWithMismatchedTypeIsIgnored(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tN int `json:\"n\" binding:\"oneof=a b\"`")), "n")
	if len(p.Enum) != 0 {
		t.Errorf("enum = %v, want none", p.Enum)
	}
}

// ---- formats ----

func TestFormats(t *testing.T) {
	cases := map[string]string{
		"email":    "email",
		"url":      "uri",
		"uuid":     "uuid",
		"ipv4":     "ipv4",
		"ipv6":     "ipv6",
		"datetime": "date-time",
	}
	for rule, want := range cases {
		t.Run(rule, func(t *testing.T) {
			p := prop(t, schemaFor(t, wrap("\tF string `json:\"f\" binding:\""+rule+"\"`")), "f")
			if p.Format != want {
				t.Errorf("format = %q, want %q", p.Format, want)
			}
		})
	}
}

// time.Time already resolves to date-time; a tag must not overwrite a format
// the type itself established.
func TestFormatFromTypeIsNotOverwritten(t *testing.T) {
	p := prop(t, schemaFor(t, wrap("\tAt time.Time `json:\"at\" binding:\"email\"`")), "at")
	if p.Format != "date-time" {
		t.Errorf("format = %q, want date-time from the type", p.Format)
	}
}

// ---- both tag spellings ----

func TestValidateTagIsReadToo(t *testing.T) {
	s := schemaFor(t, wrap("\tName string `json:\"name\" validate:\"required,min=2\"`"))
	if !contains(s.Required, "name") {
		t.Errorf("required = %v", s.Required)
	}
	if p := prop(t, s, "name"); p.MinLength == nil || *p.MinLength != 2 {
		t.Errorf("minLength = %v, want 2", p.MinLength)
	}
}

// ---- what must be ignored ----

// Rules with no JSON Schema equivalent are real constraints with nowhere to go.
// Dropping them silently is the point: refusing to generate would be worse.
func TestUnknownRulesAreIgnored(t *testing.T) {
	s := schemaFor(t, wrap("\tA string `json:\"a\" binding:\"required,gtfield=B,contains=@,customrule\"`"))
	p := prop(t, s, "a")
	if !contains(s.Required, "a") {
		t.Error("the rules we do understand must still apply")
	}
	if p.MinLength != nil || p.Minimum != nil || len(p.Enum) != 0 {
		t.Errorf("an unknown rule invented a constraint: %+v", p)
	}
}

// A typo in someone's struct tag is not a reason to stop documenting their API.
func TestMalformedTagsDoNotPanic(t *testing.T) {
	for _, tag := range []string{
		"gte=", "lte=", "min=", "max=", "len=", "oneof=",
		"gte=abc", "min=1.5.2", "=", ",,,", "required,",
		"oneof=", "len=-", "gt=+", "max= ",
	} {
		t.Run(tag, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on tag %q: %v", tag, r)
				}
			}()
			schemaFor(t, wrap("\tF string `json:\"f\" binding:\""+tag+"\"`"))
		})
	}
}

// ---- no tag: nothing changes ----

// The whole feature has to be invisible to code that does not use it, or every
// existing document churns.
func TestUntaggedStructIsUnchanged(t *testing.T) {
	s := schemaFor(t, wrap("\tName string `json:\"name\"`\n\tAge int `json:\"age\"`"))
	if len(s.Required) != 0 {
		t.Errorf("required = %v, want none", s.Required)
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, keyword := range []string{"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems", "required"} {
		if strings.Contains(string(b), keyword) {
			t.Errorf("%q appears in a document generated from untagged code: %s", keyword, b)
		}
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
