// Package conform compares a JSON value against the schema a document gives
// for it.
//
// Two things need this and must agree: the generated contract tests, which run
// in a project's own repository, and the traffic proxy, which watches a live
// API. If they disagreed, the same response could pass one and fail the other,
// and neither result would mean anything.
//
// So this file is the single source, and the contract generator copies its text
// into the code it emits rather than reimplementing it. That is why the package
// imports nothing but the standard library: the copy has to compile inside
// someone else's package, where "github.com/bakhod1r/spector/internal/..." does not
// resolve. A test locks the two together.
package conform

import (
	_ "embed"
	"fmt"
	"strings"
)

// source is this file's own text. The contract generator emits it into the
// package it writes, so the checks a project runs in its repository are
// literally the code the proxy runs — not a reimplementation that drifts.
//
//go:embed conform.go
var source string

// Source returns the package's own source, for a generator that needs to carry
// the checker somewhere this package cannot be imported from.
func Source() string { return source }

// Schema is the part of JSON Schema worth checking against a live response.
type Schema struct {
	Ref        string             `json:"$ref,omitempty"`
	Type       string             `json:"type,omitempty"`
	Items      *Schema            `json:"items,omitempty"`
	Properties map[string]*Schema `json:"properties,omitempty"`
	Required   []string           `json:"required,omitempty"`
	Enum       []any              `json:"enum,omitempty"`
	AllOf      []*Schema          `json:"allOf,omitempty"`
}

// Check reports where value contradicts schema.
//
// What it looks for is deliberately narrow: a missing required property, a
// value of the wrong type, and a value outside a documented enum. Those three
// are unambiguous evidence that the document and the service disagree —
// anything vaguer produces findings a reader cannot act on, and a checker
// nobody trusts is worse than none, because its output gets ignored wholesale.
//
// components resolves $ref; it may be nil when the schema has none.
func Check(components map[string]*Schema, s *Schema, value any, path string) []string {
	s = Deref(components, s)
	if s == nil || value == nil {
		// A null where a value was documented is worth reporting, but only when
		// the document actually said something about the shape.
		return nil
	}

	if len(s.Enum) > 0 {
		found := false
		for _, allowed := range s.Enum {
			if fmt.Sprint(allowed) == fmt.Sprint(value) {
				found = true
				break
			}
		}
		if !found {
			return []string{fmt.Sprintf("%s: %v is not one of the documented values %v", path, value, s.Enum)}
		}
	}

	switch s.Type {
	case "array":
		items, ok := value.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: documented as an array, got %s", path, KindOf(value))}
		}
		var out []string
		for i, item := range items {
			out = append(out, Check(components, s.Items, item, fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	case "object", "":
		obj, ok := value.(map[string]any)
		if !ok {
			if s.Type == "" {
				return nil // nothing was documented, so nothing is violated
			}
			return []string{fmt.Sprintf("%s: documented as an object, got %s", path, KindOf(value))}
		}
		var out []string
		for _, name := range s.Required {
			if _, present := obj[name]; !present {
				out = append(out, fmt.Sprintf("%s.%s: documented as required, missing from the response", path, name))
			}
		}
		for name, prop := range s.Properties {
			if v, present := obj[name]; present {
				out = append(out, Check(components, prop, v, path+"."+name)...)
			}
		}
		for _, part := range s.AllOf {
			out = append(out, Check(components, part, value, path)...)
		}
		return out
	case "string":
		if _, ok := value.(string); !ok {
			return []string{fmt.Sprintf("%s: documented as a string, got %s", path, KindOf(value))}
		}
	case "integer", "number":
		if _, ok := value.(float64); !ok {
			return []string{fmt.Sprintf("%s: documented as a %s, got %s", path, s.Type, KindOf(value))}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return []string{fmt.Sprintf("%s: documented as a boolean, got %s", path, KindOf(value))}
		}
	}
	return nil
}

// Undocumented lists properties the value carries that the schema does not
// mention.
//
// These are reported separately and never as failures. A response with an extra
// field usually means the API grew and the document has not caught up: real,
// worth knowing, and not a broken contract — nothing written against the
// document breaks because of a field it does not know about.
func Undocumented(components map[string]*Schema, s *Schema, value any, path string) []string {
	s = Deref(components, s)
	if s == nil {
		return nil
	}
	switch v := value.(type) {
	case []any:
		if len(v) == 0 {
			return nil
		}
		// One element is enough: a list is homogeneous, and reporting the same
		// extra field a hundred times helps nobody.
		return Undocumented(components, s.Items, v[0], path+"[0]")
	case map[string]any:
		if len(s.Properties) == 0 && len(s.AllOf) == 0 {
			return nil // nothing was documented, so nothing is missing from it
		}
		known := documentedProperties(components, s)
		var out []string
		for name := range v {
			if known[name] == nil {
				out = append(out, fmt.Sprintf("%s.%s is in the response but not in the document", path, name))
				continue
			}
			out = append(out, Undocumented(components, known[name], v[name], path+"."+name)...)
		}
		return out
	}
	return nil
}

func documentedProperties(components map[string]*Schema, s *Schema) map[string]*Schema {
	out := map[string]*Schema{}
	for name, prop := range s.Properties {
		out[name] = prop
	}
	for _, part := range s.AllOf {
		for name, prop := range documentedProperties(components, Deref(components, part)) {
			out[name] = prop
		}
	}
	return out
}

// Deref follows a $ref into the component schemas. The hop limit stops a
// document whose refs point at each other from hanging the checker.
func Deref(components map[string]*Schema, s *Schema) *Schema {
	for i := 0; s != nil && s.Ref != "" && i < 10; i++ {
		s = components[strings.TrimPrefix(s.Ref, "#/components/schemas/")]
	}
	return s
}

// KindOf names what a decoded JSON value actually is, for a message that tells
// the reader what arrived rather than only what did not.
func KindOf(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case float64:
		return "a number"
	case string:
		return "a string"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	}
	return "something else"
}
