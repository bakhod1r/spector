package core

import "encoding/json"

// ToV31 converts the document to OpenAPI 3.1. The model in this package is
// written against 3.0.3, and almost all of it is valid 3.1 unchanged; the two
// real differences that matter for what Specter emits are handled here:
//
//   - the version string becomes "3.1.0"
//   - exclusiveMinimum/exclusiveMaximum stop being booleans that modify
//     minimum/maximum and become standalone numbers, which is the JSON Schema
//     2020-12 form 3.1 adopted
//
// The result is a generic JSON tree rather than a typed Document, because the
// two spellings of exclusive bounds cannot share a struct field. Callers only
// marshal it, so the loss of type is not a loss of anything they use.
func (d *Document) ToV31() (map[string]any, error) {
	data, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	var tree map[string]any
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, err
	}
	tree["openapi"] = "3.1.0"
	fixSchemas(tree)
	return tree, nil
}

// fixSchemas walks the tree and rewrites the 3.0 exclusive-bound spelling to
// the 3.1 one wherever both keys of the pair appear together. Walking
// everything is safe: no non-schema object in the document carries an
// "exclusiveMinimum" key, so there is nothing to rewrite by accident.
func fixSchemas(v any) {
	switch node := v.(type) {
	case map[string]any:
		if excl, ok := node["exclusiveMinimum"].(bool); ok {
			if min, has := node["minimum"]; has && excl {
				node["exclusiveMinimum"] = min
				delete(node, "minimum")
			} else {
				delete(node, "exclusiveMinimum")
			}
		}
		if excl, ok := node["exclusiveMaximum"].(bool); ok {
			if max, has := node["maximum"]; has && excl {
				node["exclusiveMaximum"] = max
				delete(node, "maximum")
			} else {
				delete(node, "exclusiveMaximum")
			}
		}
		for _, child := range node {
			fixSchemas(child)
		}
	case []any:
		for _, child := range node {
			fixSchemas(child)
		}
	}
}
