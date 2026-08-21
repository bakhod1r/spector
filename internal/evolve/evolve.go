// Package evolve compares two versions of a document and classifies what
// changed by what it does to a client already using the API.
//
// The question it answers is the one a version number is supposed to encode and
// usually does not: is this change safe to ship? "Breaking" here does not mean
// "the document is different" — it means an existing, working client stops
// working. Removing an endpoint breaks the caller that used it; adding an
// optional parameter breaks nobody. The two are not the same size, and a tool
// that reported them the same way would be ignored.
//
// So every difference is judged from the consumer's side, and request and
// response are judged in opposite directions: tightening what a client may send
// is breaking, tightening what it will receive is breaking too, but the shapes
// that do each are mirror images.
package evolve

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/bakhod1r/spector/internal/core"
)

// Severity ranks a change by its effect on an existing client.
const (
	// Breaking means a client that worked against the old document can fail
	// against the new one.
	Breaking = "breaking"
	// Compatible means the change is safe but worth recording — a relaxed
	// requirement, a new optional field, a newly documented status.
	Compatible = "compatible"
	// Addition is pure new surface: nothing old is touched.
	Addition = "addition"
)

// Kinds of change. Each names one specific thing so a report can be filtered
// and a reader knows exactly what happened without parsing prose.
const (
	KindEndpointRemoved       = "endpoint-removed"
	KindEndpointAdded         = "endpoint-added"
	KindStatusRemoved         = "status-removed"
	KindStatusAdded           = "status-added"
	KindResponseFieldRemoved  = "response-field-removed"
	KindResponseFieldOptional = "response-field-now-optional"
	KindResponseFieldAdded    = "response-field-added"
	KindResponseEnumWidened   = "response-enum-widened"
	KindRequiredParamAdded    = "required-param-added"
	KindOptionalParamAdded    = "optional-param-added"
	KindParamRemoved          = "param-removed"
	KindParamNowRequired      = "param-now-required"
	KindParamNowOptional      = "param-now-optional"
	KindRequestFieldRequired  = "request-field-now-required"
	KindRequestFieldAdded     = "request-field-added"
	KindRequestEnumNarrowed   = "request-enum-narrowed"
	KindTypeChanged           = "type-changed"
	KindDeprecated            = "operation-deprecated"
)

// Change is one difference between two documents.
type Change struct {
	Severity string `json:"severity"`
	Kind     string `json:"kind"`
	Method   string `json:"method,omitempty"`
	Path     string `json:"path,omitempty"`
	Detail   string `json:"detail"`
}

func (c Change) String() string {
	loc := c.Path
	if c.Method != "" {
		loc = strings.ToUpper(c.Method) + " " + c.Path
	}
	if loc == "" {
		return fmt.Sprintf("%s: %s", strings.ToUpper(c.Severity), c.Detail)
	}
	return fmt.Sprintf("%s  %s: %s", strings.ToUpper(c.Severity), loc, c.Detail)
}

// Compare reports how newDoc differs from oldDoc, from a consumer's point of
// view. The result is ordered deterministically so regenerating it does not
// churn a diff.
func Compare(oldDoc, newDoc *core.Document) []Change {
	c := &comparer{
		old:      refResolver(oldDoc),
		new:      refResolver(newDoc),
		oldPaths: paths(oldDoc),
		newPaths: paths(newDoc),
	}

	// Endpoints, keyed by "METHOD path" so the two sides line up regardless of
	// map order.
	for key, oldOp := range c.oldPaths {
		method, path := splitKey(key)
		newOp, ok := c.newPaths[key]
		if !ok {
			c.add(Breaking, KindEndpointRemoved, method, path, "the endpoint no longer exists")
			continue
		}
		c.compareOperation(method, path, oldOp, newOp)
	}
	for key, newOp := range c.newPaths {
		if _, ok := c.oldPaths[key]; !ok {
			method, path := splitKey(key)
			summary := newOp.Summary
			if summary == "" {
				summary = "new endpoint"
			}
			c.add(Addition, KindEndpointAdded, method, path, summary)
		}
	}

	sortChanges(c.changes)
	return c.changes
}

type comparer struct {
	old, new           func(*core.Schema) *core.Schema
	oldPaths, newPaths map[string]*core.Operation
	changes            []Change
}

func (c *comparer) add(severity, kind, method, path, detail string) {
	c.changes = append(c.changes, Change{Severity: severity, Kind: kind, Method: method, Path: path, Detail: detail})
}

func (c *comparer) compareOperation(method, path string, oldOp, newOp *core.Operation) {
	if !oldOp.Deprecated && newOp.Deprecated {
		c.add(Compatible, KindDeprecated, method, path, "the operation is now deprecated")
	}
	c.compareParameters(method, path, oldOp, newOp)
	c.compareRequestBody(method, path, oldOp, newOp)
	c.compareResponses(method, path, oldOp, newOp)
}

// ---- parameters ----

func (c *comparer) compareParameters(method, path string, oldOp, newOp *core.Operation) {
	oldParams := paramMap(oldOp.Parameters)
	newParams := paramMap(newOp.Parameters)

	for key, np := range newParams {
		op, existed := oldParams[key]
		if !existed {
			if np.Required {
				c.add(Breaking, KindRequiredParamAdded, method, path,
					fmt.Sprintf("new required %s parameter %q; an existing client does not send it", np.In, np.Name))
			} else {
				c.add(Compatible, KindOptionalParamAdded, method, path,
					fmt.Sprintf("new optional %s parameter %q", np.In, np.Name))
			}
			continue
		}
		if !op.Required && np.Required {
			c.add(Breaking, KindParamNowRequired, method, path,
				fmt.Sprintf("%s parameter %q became required", np.In, np.Name))
		}
		if op.Required && !np.Required {
			c.add(Compatible, KindParamNowOptional, method, path,
				fmt.Sprintf("%s parameter %q is no longer required", np.In, np.Name))
		}
		c.compareLeaf(method, path, request, fmt.Sprintf("parameter %q", np.Name),
			c.old(op.Schema), c.new(np.Schema))
	}
	for key, op := range oldParams {
		if _, ok := newParams[key]; !ok {
			// A parameter the server stopped accepting: a client still sending
			// it is ignored, not rejected, so this is safe.
			c.add(Compatible, KindParamRemoved, method, path,
				fmt.Sprintf("%s parameter %q was removed", op.In, op.Name))
		}
	}
}

// ---- request body ----

func (c *comparer) compareRequestBody(method, path string, oldOp, newOp *core.Operation) {
	oldSchema := c.old(bodySchema(oldOp.RequestBody))
	newSchema := c.new(bodySchema(newOp.RequestBody))
	if oldSchema == nil || newSchema == nil {
		return
	}
	c.compareSchema(method, path, request, "request", oldSchema, newSchema, map[string]bool{})
}

// ---- responses ----

func (c *comparer) compareResponses(method, path string, oldOp, newOp *core.Operation) {
	for status, oldResp := range oldOp.Responses {
		newResp, ok := newOp.Responses[status]
		if !ok {
			c.add(Breaking, KindStatusRemoved, method, path,
				fmt.Sprintf("response %s is no longer documented", status))
			continue
		}
		oldSchema := c.old(jsonSchema(oldResp))
		newSchema := c.new(jsonSchema(newResp))
		if oldSchema == nil || newSchema == nil {
			continue
		}
		c.compareSchema(method, path, response, "response "+status, oldSchema, newSchema, map[string]bool{})
	}
	for status := range newOp.Responses {
		if _, ok := oldOp.Responses[status]; !ok {
			c.add(Compatible, KindStatusAdded, method, path,
				fmt.Sprintf("response %s is newly documented", status))
		}
	}
}

// direction is which way the data flows, because request and response invert
// what a change means: a client sends requests and receives responses.
type direction int

const (
	request direction = iota
	response
)

// compareSchema walks two object schemas in parallel. The recursion is bounded
// by seen, which keeps a self-referential schema (a tree, a linked list) from
// looping.
func (c *comparer) compareSchema(method, path string, dir direction, where string, oldS, newS *core.Schema, seen map[string]bool) {
	if oldS == nil || newS == nil {
		return
	}

	oldReq := stringSet(oldS.Required)
	newReq := stringSet(newS.Required)

	for name, oldProp := range oldS.Properties {
		newProp, present := newS.Properties[name]
		if !present {
			if dir == response {
				c.add(Breaking, KindResponseFieldRemoved, method, path,
					fmt.Sprintf("%s: field %q was removed; a client reading it now finds nothing", where, name))
			}
			// A request field the server stopped documenting is one the client
			// can stop sending: safe, and common enough that reporting it as a
			// change would be noise.
			continue
		}
		if dir == response && oldReq[name] && !newReq[name] {
			c.add(Breaking, KindResponseFieldOptional, method, path,
				fmt.Sprintf("%s: field %q is no longer always present", where, name))
		}
		if dir == request && !oldReq[name] && newReq[name] {
			c.add(Breaking, KindRequestFieldRequired, method, path,
				fmt.Sprintf("%s: field %q became required", where, name))
		}
		c.compareLeaf(method, path, dir, where+" field "+strconv.Quote(name),
			c.resolve(dir, oldProp), c.resolve(dir, newProp))
		c.recurse(method, path, dir, where+"."+name, oldProp, newProp, seen)
	}

	for name := range newS.Properties {
		if _, present := oldS.Properties[name]; present {
			continue
		}
		switch {
		case dir == response:
			c.add(Compatible, KindResponseFieldAdded, method, path,
				fmt.Sprintf("%s: new field %q", where, name))
		case newReq[name]:
			c.add(Breaking, KindRequestFieldRequired, method, path,
				fmt.Sprintf("%s: new required field %q; an existing client does not send it", where, name))
		default:
			c.add(Compatible, KindRequestFieldAdded, method, path,
				fmt.Sprintf("%s: new optional field %q", where, name))
		}
	}
}

func (c *comparer) recurse(method, path string, dir direction, where string, oldProp, newProp *core.Schema, seen map[string]bool) {
	oldR := c.resolve(dir, oldProp)
	newR := c.resolve(dir, newProp)
	if oldR == nil || newR == nil {
		return
	}
	// Guard on the pair of schemas being compared, not on the path to them: a
	// self-referential type (a tree's children, a linked list's next) reaches
	// the same schemas again down an ever-longer path, so a path-keyed guard
	// never fires. Identity does.
	key := schemaID(oldProp) + "\x00" + schemaID(newProp)
	if seen[key] {
		return
	}
	seen[key] = true

	if oldR.Type == "array" && newR.Type == "array" {
		c.recurse(method, path, dir, where+"[]", oldR.Items, newR.Items, seen)
		return
	}
	if len(oldR.Properties) > 0 && len(newR.Properties) > 0 {
		c.compareSchema(method, path, dir, where, oldR, newR, seen)
	}
}

// schemaID identifies a schema for cycle detection: its ref when it has one
// (two properties pointing at the same component are the same type), otherwise
// its address.
func schemaID(s *core.Schema) string {
	if s == nil {
		return "nil"
	}
	if s.Ref != "" {
		return s.Ref
	}
	return fmt.Sprintf("%p", s)
}

// compareLeaf checks the scalar facts of one field or parameter: its type and
// its enum. Nested structure is handled by the caller's recursion.
func (c *comparer) compareLeaf(method, path string, dir direction, where string, oldS, newS *core.Schema) {
	if oldS == nil || newS == nil {
		return
	}
	if oldS.Type != "" && newS.Type != "" && oldS.Type != newS.Type {
		c.add(Breaking, KindTypeChanged, method, path,
			fmt.Sprintf("%s: type changed from %s to %s", where, oldS.Type, newS.Type))
	}

	if len(oldS.Enum) == 0 && len(newS.Enum) == 0 {
		return
	}
	oldVals := enumSet(oldS.Enum)
	newVals := enumSet(newS.Enum)

	switch dir {
	case request:
		// A value the old document accepted and the new one does not: a client
		// still sending it is now rejected.
		for v := range oldVals {
			if !newVals[v] {
				c.add(Breaking, KindRequestEnumNarrowed, method, path,
					fmt.Sprintf("%s: value %q is no longer accepted", where, v))
			}
		}
	case response:
		// A value the new document can return and the old one could not: a
		// client with an exhaustive switch has no case for it.
		for v := range newVals {
			if !oldVals[v] {
				c.add(Breaking, KindResponseEnumWidened, method, path,
					fmt.Sprintf("%s: may now return %q, which a client did not expect", where, v))
			}
		}
	}
}

// resolve dereferences a schema against whichever document it came from.
func (c *comparer) resolve(dir direction, s *core.Schema) *core.Schema {
	// The direction does not decide the document; the caller passes already
	// resolved schemas where the document is known. This handles the nested
	// case, where a property carries a $ref into its own document. Both
	// resolvers are tried, because a nested property belongs to whichever side
	// produced it, and a ref that resolves in the wrong document simply returns
	// nil there.
	if s == nil || s.Ref == "" {
		return s
	}
	if r := c.old(s); r != nil && r != s {
		return r
	}
	if r := c.new(s); r != nil && r != s {
		return r
	}
	return s
}

// ---- helpers ----

// refResolver returns a function that dereferences a schema against one
// document's components, following chained refs with a hop limit so a cyclic
// document cannot hang.
func refResolver(doc *core.Document) func(*core.Schema) *core.Schema {
	var components map[string]*core.Schema
	if doc != nil {
		components = doc.Components.Schemas
	}
	return func(s *core.Schema) *core.Schema {
		for i := 0; s != nil && s.Ref != "" && i < 20; i++ {
			name := strings.TrimPrefix(s.Ref, "#/components/schemas/")
			s = components[name]
		}
		return s
	}
}

func paths(doc *core.Document) map[string]*core.Operation {
	out := map[string]*core.Operation{}
	if doc == nil {
		return out
	}
	for path, methods := range doc.Paths {
		for method, op := range methods {
			out[strings.ToUpper(method)+" "+path] = op
		}
	}
	return out
}

func splitKey(key string) (method, path string) {
	if i := strings.IndexByte(key, ' '); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}

func paramMap(params []core.Parameter) map[string]core.Parameter {
	out := map[string]core.Parameter{}
	for _, p := range params {
		out[p.In+"\x00"+p.Name] = p
	}
	return out
}

func bodySchema(body *core.RequestBody) *core.Schema {
	if body == nil {
		return nil
	}
	if media, ok := body.Content["application/json"]; ok {
		return media.Schema
	}
	return nil
}

func jsonSchema(resp *core.Response) *core.Schema {
	if resp == nil {
		return nil
	}
	if media, ok := resp.Content["application/json"]; ok {
		return media.Schema
	}
	return nil
}

func stringSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, s := range items {
		out[s] = true
	}
	return out
}

func enumSet(items []any) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, v := range items {
		out[fmt.Sprint(v)] = true
	}
	return out
}

// sortChanges orders by severity (breaking first), then location, so the report
// is stable and the most important lines are at the top.
func sortChanges(changes []Change) {
	rank := map[string]int{Breaking: 0, Compatible: 1, Addition: 2}
	sort.SliceStable(changes, func(i, j int) bool {
		a, b := changes[i], changes[j]
		if rank[a.Severity] != rank[b.Severity] {
			return rank[a.Severity] < rank[b.Severity]
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Method != b.Method {
			return a.Method < b.Method
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Detail < b.Detail
	})
}

// Summary counts changes by severity, for a one-line verdict and an exit code.
type Summary struct {
	Breaking   int `json:"breaking"`
	Compatible int `json:"compatible"`
	Addition   int `json:"addition"`
}

func Summarize(changes []Change) Summary {
	var s Summary
	for _, c := range changes {
		switch c.Severity {
		case Breaking:
			s.Breaking++
		case Compatible:
			s.Compatible++
		case Addition:
			s.Addition++
		}
	}
	return s
}
