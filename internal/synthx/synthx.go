// Package synthx generates a realistic request body for a documented
// operation.
//
// It exists because synth has two front doors that do not agree. Its OpenAPI
// one decides a field's kind from `format` alone, so every plain `type: string`
// property becomes lorem ipsum no matter what it is called, and a login body
// comes back as {"password": "eiusmod sed do", "phone_number": "do adipiscing"}.
// Its JSON Schema front door reads the field *name* first (synth's own
// infer.Kind knows "password", "phone_number", "timezone"), which is the only
// signal those fields have. So this package resolves the operation's request
// body out of the document, emits it as a plain JSON Schema, and generates from
// that — same library, the front door that reads names.
package synthx

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/bakhod1r/spector/internal/core"
	"github.com/bakhod1r/spector/internal/fieldkind"
	"github.com/bakhod1r/synth"
	"github.com/bakhod1r/synth/infer"
)

// PayloadJSON generates one request body for method+path from doc, as indented
// JSON. An error means the operation has no JSON request-body object to
// generate from — the caller's 422.
func PayloadJSON(doc *core.Document, method, path string) ([]byte, error) {
	js, err := JSONSchema(doc, method, path)
	if err != nil {
		return nil, err
	}
	spec, err := synth.JSONSchemaBytes(js)
	if err != nil {
		return nil, err
	}
	recs, err := spec.Generate(1)
	if err != nil {
		return nil, err
	}
	fillQualifiedNames(recs[0])
	return json.MarshalIndent(recs[0], "", "  ")
}

// fillQualifiedNames replaces the values synth left as prose for names it does
// not know outright. Its table has "token" and "id", not "access_token" or
// "device_id", and a real payload is mostly qualified names — so a body that
// stopped here would still read "access_token": "eiusmod sed do".
func fillQualifiedNames(rec map[string]any) {
	for name, v := range rec {
		switch t := v.(type) {
		case map[string]any:
			fillQualifiedNames(t)
		case []any:
			for _, item := range t {
				if obj, ok := item.(map[string]any); ok {
					fillQualifiedNames(obj)
				}
			}
		case string:
			// A name synth matched already generated its own kind; only the
			// ones it fell back on are worth a second look.
			if _, matched := infer.Kind(name, "string"); matched {
				continue
			}
			if s, ok := fieldkind.String(name, rand.Uint64()); ok {
				rec[name] = s
			}
		}
	}
}

// JSONSchema renders the operation's request body as a standalone JSON Schema:
// $refs resolved against the document's components, so nothing is left pointing
// at a definition the generator cannot see.
func JSONSchema(doc *core.Document, method, path string) ([]byte, error) {
	body, err := requestSchema(doc, method, path)
	if err != nil {
		return nil, err
	}
	r := &resolver{doc: doc}
	root := r.resolve(body)
	if root == nil || len(root.Properties) == 0 {
		return nil, fmt.Errorf("synthx: request body is not an object")
	}
	return json.Marshal(r.node(root, ""))
}

func requestSchema(doc *core.Document, method, path string) (*core.Schema, error) {
	if doc == nil {
		return nil, fmt.Errorf("synthx: no document")
	}
	item, ok := doc.Paths[path]
	if !ok {
		return nil, fmt.Errorf("synthx: no path %s", path)
	}
	op, ok := item[strings.ToLower(method)]
	if !ok {
		return nil, fmt.Errorf("synthx: %s has no %s", path, strings.ToUpper(method))
	}
	if op.RequestBody == nil {
		return nil, fmt.Errorf("synthx: %s %s has no request body", strings.ToUpper(method), path)
	}
	for _, ct := range []string{"application/json", "application/x-www-form-urlencoded"} {
		if mt, ok := op.RequestBody.Content[ct]; ok && mt.Schema != nil {
			return mt.Schema, nil
		}
	}
	return nil, fmt.Errorf("synthx: %s %s has no JSON request body", strings.ToUpper(method), path)
}

// resolver walks the document's schemas, following local $refs. seen guards a
// self-referential schema (a Node with a Node child) against recursing forever:
// a ref already on the stack resolves to nothing and is emitted as a bare
// object.
type resolver struct {
	doc  *core.Document
	seen map[string]bool
}

func (r *resolver) resolve(s *core.Schema) *core.Schema {
	if s == nil || s.Ref == "" {
		return s
	}
	name := s.Ref[strings.LastIndex(s.Ref, "/")+1:]
	if r.seen[name] {
		return nil
	}
	target, ok := r.doc.Components.Schemas[name]
	if !ok {
		return nil
	}
	return target
}

// enter/leave bracket a $ref expansion so a cycle is cut at the second visit.
func (r *resolver) enter(s *core.Schema) (name string, ok bool) {
	if s == nil || s.Ref == "" {
		return "", false
	}
	name = s.Ref[strings.LastIndex(s.Ref, "/")+1:]
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	r.seen[name] = true
	return name, true
}

func (r *resolver) leave(name string, ok bool) {
	if ok {
		delete(r.seen, name)
	}
}

// node renders one schema as a JSON Schema map. propName is the property this
// schema sits under — empty at the root — and is what carries the naming signal
// down to the string properties.
func (r *resolver) node(s *core.Schema, propName string) map[string]any {
	out := map[string]any{}
	if s == nil {
		return map[string]any{"type": "object"}
	}
	typ := s.Type
	if typ == "" && len(s.Properties) > 0 {
		typ = "object"
	}
	if typ == "" {
		typ = "string"
	}
	out["type"] = typ

	format := s.Format
	if format == "" && typ == "string" && isIdentifier(propName) {
		// device_id, session_id, tenant_id: synth's synonym table has bare "id"
		// but not the qualified names, and an identifier is never a sentence.
		format = "uuid"
	}
	if format != "" {
		out["format"] = format
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if s.Minimum != nil {
		out["minimum"] = *s.Minimum
	}
	if s.Maximum != nil {
		out["maximum"] = *s.Maximum
	}
	if s.MaxLength != nil {
		out["maxLength"] = *s.MaxLength
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}

	if len(s.Properties) > 0 {
		props := map[string]any{}
		for name, p := range s.Properties {
			key, entered := r.enter(p)
			props[name] = r.node(r.resolve(p), name)
			r.leave(key, entered)
		}
		out["properties"] = props
	}
	if s.Items != nil {
		key, entered := r.enter(s.Items)
		out["items"] = r.node(r.resolve(s.Items), propName)
		r.leave(key, entered)
	}
	return out
}

// isIdentifier reports whether a property name reads as an id ("device_id",
// "deviceID"), rather than merely ending in the letters i-d ("valid").
func isIdentifier(name string) bool {
	if name == "" {
		return false
	}
	n := strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(name)
	// A camelCase boundary is a word boundary too: deviceID -> device ID.
	var b strings.Builder
	for i, c := range n {
		if i > 0 && c >= 'A' && c <= 'Z' && n[i-1] >= 'a' && n[i-1] <= 'z' {
			b.WriteByte(' ')
		}
		b.WriteRune(c)
	}
	fields := strings.Fields(strings.ToLower(b.String()))
	if len(fields) == 0 {
		return false
	}
	last := fields[len(fields)-1]
	return last == "id" || last == "uuid" || last == "guid"
}
