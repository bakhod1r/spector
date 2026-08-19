package sdk

import (
	"sort"

	"github.com/bakhod1r/spector/internal/core"
)

// fieldDef is one resolved property of a schema, after allOf composition has
// been flattened in.
type fieldDef struct {
	Name     string
	Schema   *core.Schema
	Required bool
}

// resolvedFields returns every property a schema carries, with its allOf
// members flattened in ahead of the type's own properties. This is how the
// dynamically-typed and record-style emitters compose: rather than a language
// inheritance construct, the base type's fields are laid directly into the
// composing type, which always produces a client that can see them.
//
// Composed (allOf) fields come first, in the order the allOf lists them, then
// the type's own properties sorted for a stable output. A name that appears in
// both keeps its first (composed) definition and is not repeated.
func resolvedFields(doc *core.Document, s *core.Schema) []fieldDef {
	var out []fieldDef
	seen := map[string]bool{}

	add := func(name string, sc *core.Schema, req bool) {
		if seen[name] {
			return
		}
		seen[name] = true
		out = append(out, fieldDef{Name: name, Schema: sc, Required: req})
	}

	for _, a := range s.AllOf {
		base := a
		if a.Ref != "" && doc != nil {
			if r, ok := doc.Components.Schemas[refName(a.Ref)]; ok {
				base = r
			}
		}
		for _, f := range resolvedFields(doc, base) {
			add(f.Name, f.Schema, f.Required)
		}
	}

	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	names := make([]string, 0, len(s.Properties))
	for p := range s.Properties {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		add(p, s.Properties[p], required[p])
	}
	return out
}

// isStruct reports whether a named schema should render as a composite type (a
// class/struct/interface) rather than a type alias. It is a struct when it has
// its own properties or composes others through allOf.
func isStruct(s *core.Schema) bool {
	return len(s.Properties) > 0 || len(s.AllOf) > 0
}

// aliasTarget returns the schema an allOf-of-one-ref aliases, or nil when the
// schema is not such an alias.
func aliasTarget(s *core.Schema) *core.Schema {
	if len(s.AllOf) == 1 && len(s.Properties) == 0 && s.Type != "object" && s.AllOf[0].Ref != "" {
		return s.AllOf[0]
	}
	return nil
}
