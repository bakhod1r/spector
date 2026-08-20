package astutil

import "github.com/bakhod1r/spector/internal/core"

// envelopes rewrites responses that a project's own helper wrapped.
//
// Almost every Go service answers through one envelope type:
//
//	func OK(c *gin.Context, data any) { c.JSON(200, Envelope{Data: data}) }
//
// Read literally, every endpoint in such a project returns `Envelope`, whose
// `data` is `any` and therefore documents nothing. The document then lists
// eighty operations that all return the same shape, and a client generated
// from it has no types at all — the failure is silent, because the document
// looks complete.
//
// The scan already knows the payload the helper was handed. This registers a
// schema for the pairing — Envelope with `data` replaced by that payload — and
// points the response at it. The envelope's own fields (meta, error) survive,
// because the endpoint really does return them.
type envelopes struct {
	schemas map[string]*core.Schema
	fields  map[string]map[string]string
}

func (e envelopes) apply(h Handler) Handler {
	h.Response = e.resolve(h.Response)
	for i := range h.Responses {
		h.Responses[i].Type = e.resolve(h.Responses[i].Type)
	}
	return h
}

func (e envelopes) resolve(t TypeInfo) TypeInfo {
	if t.Wrap == nil || t.Wrap.Inner.Name == "" {
		return t
	}
	name, ok := e.register(t.Name, *t.Wrap)
	if !ok {
		return t
	}
	return TypeInfo{Name: name, Array: t.Array}
}

// register creates the wrapped schema, or reports that it cannot: an envelope
// whose struct was never scanned, or whose payload field is not serialized,
// leaves the response exactly as it was rather than inventing a type.
func (e envelopes) register(outer string, w Wrapped) (string, bool) {
	base, ok := e.schemas[outer]
	if !ok || base.Type != "object" || len(base.Properties) == 0 {
		return "", false
	}
	prop, ok := e.fields[outer][w.Field]
	if !ok {
		return "", false
	}
	if _, present := base.Properties[prop]; !present {
		return "", false
	}
	name := outer + "Of" + w.Inner.Name
	if w.Inner.Array {
		name += "List"
	}
	if _, done := e.schemas[name]; done {
		return name, true
	}
	// The payload's own schema has to exist, or the reference would dangle.
	if _, known := e.schemas[w.Inner.Name]; !known {
		return "", false
	}

	clone := *base
	clone.Properties = make(map[string]*core.Schema, len(base.Properties))
	for k, v := range base.Properties {
		clone.Properties[k] = v
	}
	ref := &core.Schema{Ref: "#/components/schemas/" + w.Inner.Name}
	if w.Inner.Array {
		ref = &core.Schema{Type: "array", Items: ref}
	}
	clone.Properties[prop] = ref
	e.schemas[name] = &clone
	return name, true
}
