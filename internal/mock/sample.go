package mock

import (
	"strings"

	"github.com/user/specter/internal/core"
)

// Sample builds a value for a schema.
//
// The rule it follows is that the value must satisfy the schema it came from.
// A mock that answers with data its own document would reject is worse than no
// mock: a client written against it passes locally and fails on the real API,
// which is precisely the failure a mock exists to prevent. So enum, format,
// bounds and lengths are all honoured rather than decorated over.
//
// params are the path parameters from the request, so GET /users/42 answers
// with id 42 instead of a fabricated one. Echoing the caller's own value back is
// the one piece of realism available without inventing state.
func Sample(doc *core.Document, schema *core.Schema, params map[string]string) any {
	return sample(doc, schema, params, map[string]bool{}, 0)
}

// maxDepth stops a self-referential schema (a Category with children, a Node
// with a parent) from recursing forever. The cycle guard alone is not enough:
// two schemas can alternate without either repeating in one branch.
//
// It has to be generous, because every $ref and every nested object costs a
// level: Cart -> items -> LineItem -> price -> Money -> amount is already five
// for a perfectly ordinary model. Too low and fields come back null, which
// looks like a bug in the API rather than a limit in the mock.
const maxDepth = 20

func sample(doc *core.Document, schema *core.Schema, params map[string]string, seen map[string]bool, depth int) any {
	if schema == nil || depth > maxDepth {
		return nil
	}

	if schema.Ref != "" {
		name := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
		if seen[name] {
			// A recursive field becomes null rather than an ever-deeper object.
			return nil
		}
		resolved := resolve(doc, name)
		if resolved == nil {
			return map[string]any{}
		}
		seen[name] = true
		defer delete(seen, name)
		return sample(doc, resolved, params, seen, depth+1)
	}

	// Composed schemas are merged: allOf means the value satisfies all of them,
	// so one flat object carrying every member is the only correct answer.
	if len(schema.AllOf) > 0 {
		merged := map[string]any{}
		for _, part := range schema.AllOf {
			if sub, ok := sample(doc, part, params, seen, depth+1).(map[string]any); ok {
				for k, v := range sub {
					merged[k] = v
				}
			}
		}
		for name, prop := range schema.Properties {
			merged[name] = sample(doc, prop, params, seen, depth+1)
		}
		return merged
	}

	// An enum is the tightest constraint there is: any value outside it is
	// invalid, so it wins over every other rule.
	if len(schema.Enum) > 0 {
		return schema.Enum[0]
	}

	switch schema.Type {
	case "object":
		obj := map[string]any{}
		for name, prop := range schema.Properties {
			// A property named like a path parameter is filled from the request,
			// so the response is about the resource that was asked for.
			if v, ok := params[name]; ok {
				obj[name] = coerce(prop, v)
				continue
			}
			obj[name] = sample(doc, prop, params, seen, depth+1)
		}
		return obj

	case "array":
		n := 1
		if schema.MinItems != nil && *schema.MinItems > n {
			n = *schema.MinItems
		}
		if schema.MaxItems != nil && *schema.MaxItems < n {
			n = *schema.MaxItems
		}
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, sample(doc, schema.Items, params, seen, depth+1))
		}
		return out

	case "integer":
		return int(numberInRange(schema, 1))
	case "number":
		return numberInRange(schema, 1.5)
	case "boolean":
		return true
	case "string":
		return stringValue(schema)
	}
	return nil
}

// numberInRange returns a value the bounds actually allow. Returning a constant
// would break the moment a field says minimum: 18.
func numberInRange(schema *core.Schema, fallback float64) float64 {
	v := fallback
	if schema.Minimum != nil {
		v = *schema.Minimum
		if schema.ExclusiveMinimum {
			v++
		}
	}
	if schema.Maximum != nil {
		max := *schema.Maximum
		if schema.ExclusiveMaximum {
			max--
		}
		if v > max {
			v = max
		}
	}
	return v
}

// formatSamples are values that actually parse as what the format claims. A
// client that validates its input — and generated clients increasingly do —
// rejects "string" where a uuid is documented.
var formatSamples = map[string]string{
	"email":     "user@example.com",
	"uri":       "https://example.com",
	"uuid":      "3f6c1e5a-9d2b-4a7f-8c31-0b5e2f9a7c14",
	"date-time": "2024-01-15T09:30:00Z",
	"date":      "2024-01-15",
	"ipv4":      "192.0.2.1",
	"ipv6":      "2001:db8::1",
	"hostname":  "example.com",
	"byte":      "c3BlY3Rlcg==",
}

func stringValue(schema *core.Schema) string {
	if s, ok := formatSamples[schema.Format]; ok {
		return s
	}
	s := "string"
	// The length bounds are part of the schema too, so a value that violates
	// them is as wrong as one of the wrong type.
	if schema.MinLength != nil && len(s) < *schema.MinLength {
		s = s + strings.Repeat("x", *schema.MinLength-len(s))
	}
	if schema.MaxLength != nil && len(s) > *schema.MaxLength {
		s = s[:*schema.MaxLength]
	}
	return s
}

// coerce converts a path parameter, which arrives as text, to the type the
// schema declares — otherwise a numeric id comes back quoted.
func coerce(schema *core.Schema, raw string) any {
	if schema == nil {
		return raw
	}
	switch schema.Type {
	case "integer":
		n := 0
		neg := false
		for i, r := range raw {
			if i == 0 && r == '-' {
				neg = true
				continue
			}
			if r < '0' || r > '9' {
				return raw // not a number after all; echo it unchanged
			}
			n = n*10 + int(r-'0')
		}
		if neg {
			n = -n
		}
		return n
	case "number", "boolean":
		// Left as text: a malformed value would be a worse answer than an
		// honest string, and path parameters are documented as strings anyway.
		return raw
	}
	return raw
}

func resolve(doc *core.Document, name string) *core.Schema {
	if doc == nil || doc.Components.Schemas == nil {
		return nil
	}
	return doc.Components.Schemas[name]
}
