package gen

import (
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/user/specter/internal/core"
)

const refPrefix = "#/components/schemas/"

// builder threads the schema registry and the set of referenced schemas
// through operation construction so each route can pull in exactly the
// component schemas it touches.
type builder struct {
	schemas map[string]*core.Schema
	used    map[string]bool
	// doc is needed because shared parameters are defined once in components
	// and referenced from each operation that requires them.
	doc *core.Document
}

func Build(title, version string, routes []core.Route, schemas map[string]*core.Schema) *core.Document {
	doc := core.NewDocument(title, version)
	b := &builder{schemas: schemas, used: map[string]bool{}, doc: doc}

	for _, route := range routes {
		doc.AddOperation(route.Path, route.Method, b.operation(route))
	}

	for name := range b.used {
		if s := schemas[name]; s != nil {
			doc.Components.Schemas[name] = s
		}
	}
	pruneDanglingRefs(doc)
	return doc
}

// pruneDanglingRefs drops references to schemas that are not in the document.
// They arise from embedding or naming a type the scan never saw (sync.Mutex,
// gorm.Model): the reference is written by name before it is known whether
// that name resolves, and only here is the full set of names available.
// A dangling $ref makes the document fail OpenAPI validation outright, so it
// is better to lose the detail than to emit something unusable.
func pruneDanglingRefs(doc *core.Document) {
	known := func(ref string) bool {
		name := strings.TrimPrefix(ref, refPrefix)
		_, ok := doc.Components.Schemas[name]
		return ok
	}

	seen := map[*core.Schema]bool{}
	var walk func(s *core.Schema)
	walk = func(s *core.Schema) {
		if s == nil || seen[s] {
			return // schemas may reference each other; visit each once
		}
		seen[s] = true

		if len(s.AllOf) > 0 {
			kept := s.AllOf[:0]
			for _, sub := range s.AllOf {
				if sub.Ref != "" && !known(sub.Ref) {
					continue
				}
				kept = append(kept, sub)
				walk(sub)
			}
			s.AllOf = kept
			// A schema that composed only unknown types is back to a plain
			// object; without this it would serialise as neither.
			if len(s.AllOf) == 0 {
				s.AllOf = nil
				if s.Type == "" && s.Properties == nil {
					s.Type = "object"
				}
			}
		}

		// An unresolvable named type degrades to an untyped object rather than
		// pointing at nothing.
		demote := func(child *core.Schema) {
			if child != nil && child.Ref != "" && !known(child.Ref) {
				child.Ref = ""
				child.Type = "object"
			}
		}
		demote(s.Items)
		demote(s.AdditionalProperties)
		for _, p := range s.Properties {
			demote(p)
		}

		walk(s.Items)
		walk(s.AdditionalProperties)
		for _, p := range s.Properties {
			walk(p)
		}
	}

	for _, s := range doc.Components.Schemas {
		walk(s)
	}
}

// operationID falls back to method+path when the route was registered with an
// expression rather than a named function (an inline closure, or a handler
// returned by a call). Codegen and client generators key off operationId, so
// leaving it empty costs the caller more than a synthetic name does.
func operationID(route core.Route) string {
	if route.OperationID != "" {
		return route.OperationID // declared explicitly; nothing to infer
	}
	if route.HandlerName != "" {
		return route.HandlerName
	}
	parts := []string{strings.ToLower(route.Method)}
	for _, seg := range strings.Split(route.Path, "/") {
		if seg == "" {
			continue
		}
		// {id} becomes ById so the name reads as one identifier.
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			seg = "By" + upperFirst(strings.Trim(seg, "{}"))
		}
		parts = append(parts, sanitizeSegment(seg))
	}
	return strings.Join(parts, "_")
}

// securityFor turns authentication middleware into OpenAPI security
// requirements. Separate entries in one requirement object mean "all of these",
// which is what a chain of two auth middlewares actually is.
func securityFor(route core.Route) []core.SecurityRequirement {
	req := core.SecurityRequirement{}
	for _, m := range route.Middleware {
		if m.Scheme != "" {
			req[m.Scheme] = []string{}
		}
	}
	if len(req) == 0 {
		return nil
	}
	return []core.SecurityRequirement{req}
}

// upperFirst capitalizes the first rune. strings.Title is deprecated and does
// more than is wanted here.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// sanitizeSegment keeps a path segment usable as part of an identifier.
func sanitizeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// operation assembles the OpenAPI operation for a single route via the core
// functional-options constructors.
func (b *builder) operation(route core.Route) *core.Operation {
	op := core.NewOperation(operationID(route),
		core.WithSummary(route.Summary),
		core.WithDescription(route.Description),
		core.WithSource(route.Source),
		core.WithCalls(route.Calls),
		core.WithRealtime(route.Realtime),
		core.WithMiddleware(route.Middleware),
		core.WithSecurity(securityFor(route)),
		core.WithTags(route.Tags),
		core.WithDeprecated(route.Deprecated),
	)

	for _, name := range pathParams(route.Path) {
		core.WithParameter(core.Parameter{
			Name: name, In: "path", Required: true, Schema: &core.Schema{Type: "string"},
		})(op)
	}
	for _, name := range route.QueryParams {
		core.WithParameter(core.Parameter{
			Name: name, In: "query", Schema: &core.Schema{Type: "string"},
		})(op)
	}
	for _, name := range route.HeaderParams {
		core.WithParameter(core.Parameter{
			Name: name, In: "header", Schema: &core.Schema{Type: "string"},
		})(op)
	}

	if b.has(route.RequestType) {
		core.WithRequestBody(&core.RequestBody{
			Required: true,
			Content: map[string]core.MediaType{
				"application/json": {Schema: refSchema(route.RequestType, route.RequestArray)},
			},
		})(op)
		b.reference(route.RequestType)
	}

	b.responses(op, route)
	b.applyMiddleware(op, route)
	return op
}

// applyMiddleware documents what the guards in front of a handler demand and
// how they refuse. These are the two things a client author most needs and the
// two a handler-only reading can never see: the headers are required on every
// request, and the statuses can come back from an endpoint whose own code
// never produces them.
func (b *builder) applyMiddleware(op *core.Operation, route core.Route) {
	existing := map[string]bool{}
	for _, p := range op.Parameters {
		existing[strings.ToLower(p.Name)] = true
	}

	for _, m := range route.Middleware {
		for _, h := range m.Headers {
			if existing[strings.ToLower(h)] {
				continue // the handler reads it too; one parameter, not two
			}
			existing[strings.ToLower(h)] = true
			// Defined once and referenced, rather than repeated: a guard on a
			// hundred routes would otherwise write the same four parameters a
			// hundred times, and a reader could not tell they were the same
			// requirement rather than four coincidences.
			core.WithParameter(core.Parameter{Ref: b.sharedHeader(h, m.Name)})(op)
		}

		for _, status := range m.Statuses {
			code := strconv.Itoa(status)
			if _, has := op.Responses[code]; has {
				continue // the handler documents this status itself
			}
			op.SetResponse(status, core.NewResponse(middlewareStatusText(status, m.Name)))
		}
	}
}

// sharedHeader defines a header parameter in components on first use and
// returns a reference to it.
func (b *builder) sharedHeader(name, from string) string {
	key := parameterKey(name)
	if b.doc.Components.Parameters == nil {
		b.doc.Components.Parameters = map[string]*core.Parameter{}
	}
	if _, defined := b.doc.Components.Parameters[key]; !defined {
		b.doc.Components.Parameters[key] = &core.Parameter{
			Name:        name,
			In:          "header",
			Required:    true,
			Schema:      &core.Schema{Type: "string"},
			Description: "Required by " + from + ".",
		}
	}
	return paramRefPrefix + key
}

const paramRefPrefix = "#/components/parameters/"

// parameterKey turns a header name into a component key. OpenAPI restricts
// these to letters, digits, dots, hyphens and underscores; hyphens are dropped
// so X-Request-ID reads as XRequestID rather than X-Request-ID.
func parameterKey(header string) string {
	var b strings.Builder
	for _, r := range header {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "Header"
	}
	return b.String()
}

// middlewareStatusText names the response and says where it comes from. A bare
// "Bad Request" on an endpoint whose handler cannot produce one is confusing;
// naming the middleware makes it obvious.
func middlewareStatusText(status int, name string) string {
	text := statusText(status)
	if text == "Response" {
		// A non-standard code, which is exactly the case worth explaining:
		// nothing else in the document would tell a client that 488 exists.
		return "Rejected by " + name
	}
	return text + " (from " + name + ")"
}

// responses fills op.Responses. Status-coded responses discovered from the
// handler (c.JSON(code, ...) / w.WriteHeader(code)) each become an entry;
// absent those, it falls back to a single 200 keyed off ResponseType.
func (b *builder) responses(op *core.Operation, route core.Route) {
	if len(route.Responses) > 0 {
		for _, r := range route.Responses {
			op.SetResponse(r.Status, b.response(r.Status, r.Type, r.Array))
		}
		return
	}
	op.SetResponse(200, b.response(200, route.ResponseType, route.ResponseArray))
}

// response builds one status-coded response, referencing the body schema when
// it is known.
func (b *builder) response(status int, typ string, array bool) *core.Response {
	if !b.has(typ) {
		return core.NewResponse(statusText(status))
	}
	b.reference(typ)
	return core.NewResponse(statusText(status), core.WithJSONBody(refSchema(typ, array)))
}

// has reports whether a named schema exists in the registry.
func (b *builder) has(name string) bool {
	return name != "" && b.schemas[name] != nil
}

// statusText returns the standard reason phrase for a status code, falling
// back to a generic label for non-standard codes.
func statusText(status int) string {
	if t := http.StatusText(status); t != "" {
		return t
	}
	return "Response"
}

func refSchema(name string, array bool) *core.Schema {
	ref := &core.Schema{Ref: refPrefix + name}
	if array {
		return &core.Schema{Type: "array", Items: ref}
	}
	return ref
}

func pathParams(path string) []string {
	var params []string
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			params = append(params, part[1:len(part)-1])
		}
	}
	return params
}

// reference marks a named schema as used and recursively pulls in every schema
// it depends on, so the emitted components section is exactly the transitive
// closure of what the API touches.
func (b *builder) reference(name string) {
	if name == "" || b.used[name] {
		return
	}
	schema := b.schemas[name]
	if schema == nil {
		return
	}
	b.used[name] = true
	b.walk(schema)
}

func (b *builder) walk(schema *core.Schema) {
	if schema == nil {
		return
	}
	if schema.Ref != "" {
		b.reference(strings.TrimPrefix(schema.Ref, refPrefix))
	}
	if schema.Items != nil {
		b.walk(schema.Items)
	}
	for _, prop := range schema.Properties {
		b.walk(prop)
	}
	for _, s := range schema.AllOf {
		b.walk(s)
	}
}
