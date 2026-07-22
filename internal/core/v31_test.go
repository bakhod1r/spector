package core

import "testing"

func fnum(v float64) *float64 { return &v }

// A 3.0 document converts to 3.1: the version string changes and the exclusive
// bounds stop being booleans that modify minimum/maximum.
func TestToV31RewritesExclusiveBounds(t *testing.T) {
	doc := &Document{
		OpenAPI: "3.0.3",
		Info:    Info{Title: "T", Version: "1"},
		Paths:   map[string]map[string]*Operation{},
		Components: Components{Schemas: map[string]*Schema{
			"Bounded": {
				Type: "object",
				Properties: map[string]*Schema{
					"ratio": {
						Type:             "number",
						Minimum:          fnum(0),
						Maximum:          fnum(1),
						ExclusiveMinimum: true,
						ExclusiveMaximum: true,
					},
					"tags": {Type: "array", Items: &Schema{Type: "string", Enum: []any{"a", "b"}}},
				},
			},
		}},
	}

	tree, err := doc.ToV31()
	if err != nil {
		t.Fatal(err)
	}
	if tree["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v, want 3.1.0", tree["openapi"])
	}

	schemas := tree["components"].(map[string]any)["schemas"].(map[string]any)
	props := schemas["Bounded"].(map[string]any)["properties"].(map[string]any)
	ratio := props["ratio"].(map[string]any)
	if _, has := ratio["minimum"]; has {
		t.Error("minimum survived the 3.1 conversion")
	}
	if _, has := ratio["maximum"]; has {
		t.Error("maximum survived the 3.1 conversion")
	}
	if got, ok := ratio["exclusiveMinimum"].(float64); !ok || got != 0 {
		t.Errorf("exclusiveMinimum = %v, want the numeric 0", ratio["exclusiveMinimum"])
	}
	if got, ok := ratio["exclusiveMaximum"].(float64); !ok || got != 1 {
		t.Errorf("exclusiveMaximum = %v, want the numeric 1", ratio["exclusiveMaximum"])
	}
	// The array walk must reach schemas nested under a list-valued key.
	if _, ok := props["tags"].(map[string]any)["items"]; !ok {
		t.Error("items lost")
	}
}

// An exclusive flag with no bound to move is meaningless in 3.1, so it is
// dropped rather than emitted as a stray boolean.
func TestToV31DropsExclusiveWithoutBound(t *testing.T) {
	doc := &Document{
		OpenAPI: "3.0.3",
		Paths:   map[string]map[string]*Operation{},
		Components: Components{Schemas: map[string]*Schema{
			"Loose": {Type: "number", ExclusiveMinimum: true, ExclusiveMaximum: true},
		}},
	}
	tree, err := doc.ToV31()
	if err != nil {
		t.Fatal(err)
	}
	loose := tree["components"].(map[string]any)["schemas"].(map[string]any)["Loose"].(map[string]any)
	if _, has := loose["exclusiveMinimum"]; has {
		t.Error("bare exclusiveMinimum was kept")
	}
	if _, has := loose["exclusiveMaximum"]; has {
		t.Error("bare exclusiveMaximum was kept")
	}
}

// fixSchemas walks lists as well as objects: a schema inside allOf gets the
// same rewrite as one at the top level.
func TestToV31WalksLists(t *testing.T) {
	doc := &Document{
		Paths: map[string]map[string]*Operation{},
		Components: Components{Schemas: map[string]*Schema{
			"Composed": {AllOf: []*Schema{{Type: "number", Minimum: fnum(3), ExclusiveMinimum: true}}},
		}},
	}
	tree, err := doc.ToV31()
	if err != nil {
		t.Fatal(err)
	}
	all := tree["components"].(map[string]any)["schemas"].(map[string]any)["Composed"].(map[string]any)["allOf"].([]any)
	member := all[0].(map[string]any)
	if _, has := member["minimum"]; has {
		t.Error("minimum inside allOf was not rewritten")
	}
	if got, ok := member["exclusiveMinimum"].(float64); !ok || got != 3 {
		t.Errorf("exclusiveMinimum inside allOf = %v, want 3", member["exclusiveMinimum"])
	}
}
