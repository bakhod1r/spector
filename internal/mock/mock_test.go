package mock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/user/specter/internal/core"
)

func ptrF(f float64) *float64 { return &f }
func ptrI(n int) *int         { return &n }

func docWith(schemas map[string]*core.Schema, path, method string, op *core.Operation) *core.Document {
	d := core.NewDocument("t", "1")
	d.Components.Schemas = schemas
	d.AddOperation(path, method, op)
	return d
}

func okOp(ref string) *core.Operation {
	op := core.NewOperation("x")
	op.SetResponse(200, core.NewResponse("OK", core.WithJSONBody(&core.Schema{Ref: "#/components/schemas/" + ref})))
	return op
}

func get(t *testing.T, doc *core.Document, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	Handler(doc).ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func body(t *testing.T, w *httptest.ResponseRecorder) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("%v\n%s", err, w.Body.String())
	}
	return v
}

// ---- the property the whole package rests on ----

// validate is a small checker for our own schema model. A mock that answers
// with data its own document rejects is worse than no mock: a client written
// against it passes locally and fails against the real API, which is exactly
// the failure a mock exists to prevent.
func validate(t *testing.T, doc *core.Document, schema *core.Schema, v any, path string) {
	t.Helper()
	if schema == nil {
		return
	}
	if schema.Ref != "" {
		name := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
		if v == nil {
			return // a cycle was cut; null is the documented escape
		}
		validate(t, doc, doc.Components.Schemas[name], v, path)
		return
	}
	if len(schema.AllOf) > 0 {
		for _, part := range schema.AllOf {
			validate(t, doc, part, v, path)
		}
		return
	}
	if len(schema.Enum) > 0 {
		for _, e := range schema.Enum {
			if fmt.Sprint(e) == fmt.Sprint(v) {
				return
			}
		}
		t.Errorf("%s: %v is not in the enum %v", path, v, schema.Enum)
		return
	}

	switch schema.Type {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			t.Errorf("%s: got %T, want an object", path, v)
			return
		}
		for _, req := range schema.Required {
			if _, present := obj[req]; !present {
				t.Errorf("%s: required property %q is missing", path, req)
			}
		}
		for name, prop := range schema.Properties {
			validate(t, doc, prop, obj[name], path+"."+name)
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			t.Errorf("%s: got %T, want an array", path, v)
			return
		}
		if schema.MinItems != nil && len(arr) < *schema.MinItems {
			t.Errorf("%s: %d items, want at least %d", path, len(arr), *schema.MinItems)
		}
		if schema.MaxItems != nil && len(arr) > *schema.MaxItems {
			t.Errorf("%s: %d items, want at most %d", path, len(arr), *schema.MaxItems)
		}
		for i, item := range arr {
			validate(t, doc, schema.Items, item, fmt.Sprintf("%s[%d]", path, i))
		}
	case "string":
		s, ok := v.(string)
		if !ok {
			t.Errorf("%s: got %T, want a string", path, v)
			return
		}
		if schema.MinLength != nil && len(s) < *schema.MinLength {
			t.Errorf("%s: %q is shorter than minLength %d", path, s, *schema.MinLength)
		}
		if schema.MaxLength != nil && len(s) > *schema.MaxLength {
			t.Errorf("%s: %q is longer than maxLength %d", path, s, *schema.MaxLength)
		}
	case "integer", "number":
		f, ok := v.(float64)
		if !ok {
			t.Errorf("%s: got %T, want a number", path, v)
			return
		}
		if schema.Minimum != nil {
			if (schema.ExclusiveMinimum && f <= *schema.Minimum) || (!schema.ExclusiveMinimum && f < *schema.Minimum) {
				t.Errorf("%s: %v violates minimum %v (exclusive=%v)", path, f, *schema.Minimum, schema.ExclusiveMinimum)
			}
		}
		if schema.Maximum != nil {
			if (schema.ExclusiveMaximum && f >= *schema.Maximum) || (!schema.ExclusiveMaximum && f > *schema.Maximum) {
				t.Errorf("%s: %v violates maximum %v (exclusive=%v)", path, f, *schema.Maximum, schema.ExclusiveMaximum)
			}
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			t.Errorf("%s: got %T, want a boolean", path, v)
		}
	}
}

// A schema exercising every constraint at once, so the generated value has to
// satisfy all of them simultaneously rather than one rule at a time.
func constrainedDoc() *core.Document {
	schemas := map[string]*core.Schema{
		"Thing": {
			Type:     "object",
			Required: []string{"id", "email", "tier", "tags"},
			Properties: map[string]*core.Schema{
				"id":     {Type: "integer", Minimum: ptrF(10), Maximum: ptrF(20)},
				"score":  {Type: "number", Minimum: ptrF(0), ExclusiveMinimum: true, Maximum: ptrF(1)},
				"email":  {Type: "string", Format: "email"},
				"code":   {Type: "string", MinLength: ptrI(8), MaxLength: ptrI(8)},
				"short":  {Type: "string", MaxLength: ptrI(3)},
				"tier":   {Type: "string", Enum: []any{"free", "pro"}},
				"tags":   {Type: "array", MinItems: ptrI(2), MaxItems: ptrI(3), Items: &core.Schema{Type: "string"}},
				"active": {Type: "boolean"},
				"nested": {Ref: "#/components/schemas/Inner"},
			},
		},
		"Inner": {
			Type:       "object",
			Properties: map[string]*core.Schema{"at": {Type: "string", Format: "date-time"}},
		},
	}
	return docWith(schemas, "/things", "get", okOp("Thing"))
}

func TestMockOutputSatisfiesItsOwnSchema(t *testing.T) {
	doc := constrainedDoc()
	w := get(t, doc, http.MethodGet, "/things")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	validate(t, doc, &core.Schema{Ref: "#/components/schemas/Thing"}, body(t, w), "Thing")
}

// The format samples have to be values that actually parse as that format. A
// generated client that validates its input rejects "string" where a uuid is
// documented.
func TestFormatsProduceParseableValues(t *testing.T) {
	for format, want := range map[string]string{
		"email":     "@",
		"uri":       "https://",
		"date-time": "T",
		"uuid":      "-",
	} {
		t.Run(format, func(t *testing.T) {
			got := Sample(nil, &core.Schema{Type: "string", Format: format}, nil)
			s, ok := got.(string)
			if !ok || !strings.Contains(s, want) {
				t.Errorf("= %v, want something containing %q", got, want)
			}
		})
	}
}

// ---- routing ----

// Echoing the caller's own id back is the one piece of realism available
// without inventing state, and it is what makes a list-then-detail flow in a
// frontend look right.
func TestPathParameterIsEchoedBack(t *testing.T) {
	doc := docWith(map[string]*core.Schema{
		"User": {Type: "object", Properties: map[string]*core.Schema{
			"id":   {Type: "integer"},
			"name": {Type: "string"},
		}},
	}, "/users/{id}", "get", okOp("User"))

	obj := body(t, get(t, doc, http.MethodGet, "/users/42")).(map[string]any)
	if obj["id"] != float64(42) {
		t.Errorf("id = %v, want the 42 from the path", obj["id"])
	}
}

// A quoted id would break any client that expects a number.
func TestPathParameterIsTypedNotQuoted(t *testing.T) {
	doc := docWith(map[string]*core.Schema{
		"User": {Type: "object", Properties: map[string]*core.Schema{"id": {Type: "integer"}}},
	}, "/users/{id}", "get", okOp("User"))

	raw := get(t, doc, http.MethodGet, "/users/7").Body.String()
	if strings.Contains(raw, `"id": "7"`) {
		t.Errorf("id was quoted: %s", raw)
	}
}

// A non-numeric id is echoed rather than mangled into a number.
func TestNonNumericPathParameterIsEchoedAsText(t *testing.T) {
	doc := docWith(map[string]*core.Schema{
		"User": {Type: "object", Properties: map[string]*core.Schema{"id": {Type: "integer"}}},
	}, "/users/{id}", "get", okOp("User"))

	obj := body(t, get(t, doc, http.MethodGet, "/users/abc")).(map[string]any)
	if obj["id"] != "abc" {
		t.Errorf("id = %v, want abc echoed unchanged", obj["id"])
	}
}

// A literal path must win over a parameterised one that also matches, and it
// must do so on every run — map iteration order cannot be allowed to decide.
func TestLiteralPathBeatsParameterConsistently(t *testing.T) {
	schemas := map[string]*core.Schema{
		"Me":   {Type: "object", Properties: map[string]*core.Schema{"kind": {Type: "string", Enum: []any{"me"}}}},
		"User": {Type: "object", Properties: map[string]*core.Schema{"kind": {Type: "string", Enum: []any{"user"}}}},
	}
	doc := core.NewDocument("t", "1")
	doc.Components.Schemas = schemas
	doc.AddOperation("/users/{id}", "get", okOp("User"))
	doc.AddOperation("/users/me", "get", okOp("Me"))

	for i := 0; i < 20; i++ {
		obj := body(t, get(t, doc, http.MethodGet, "/users/me")).(map[string]any)
		if obj["kind"] != "me" {
			t.Fatalf("run %d: /users/me was served by the {id} operation", i)
		}
	}
}

func TestUndocumentedPathIsAProblemDocument(t *testing.T) {
	w := get(t, constrainedDoc(), http.MethodGet, "/nope")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if detail := body(t, w).(map[string]any)["detail"]; !strings.Contains(fmt.Sprint(detail), "/nope") {
		t.Errorf("detail does not name the path: %v", detail)
	}
}

func TestWrongMethodIsNotMatched(t *testing.T) {
	if code := get(t, constrainedDoc(), http.MethodPost, "/things").Code; code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

// ---- status selection ----

// The lowest 2xx is the success case; a client should not have to ask for it.
func TestDefaultStatusIsTheLowest2xx(t *testing.T) {
	op := core.NewOperation("x")
	op.SetResponse(201, core.NewResponse("Created"))
	op.SetResponse(400, core.NewResponse("Bad Request"))
	if code := get(t, docWith(nil, "/x", "post", op), http.MethodPost, "/x").Code; code != 201 {
		t.Errorf("status = %d, want 201", code)
	}
}

// Exercising error handling is most of what a frontend needs a mock for, and
// the alternative is waiting for the real API to fail on its own.
func TestStatusCanBeForced(t *testing.T) {
	op := core.NewOperation("x")
	op.SetResponse(200, core.NewResponse("OK"))
	op.SetResponse(503, core.NewResponse("Service Unavailable"))
	doc := docWith(nil, "/x", "get", op)

	if code := get(t, doc, http.MethodGet, "/x?__status=503").Code; code != 503 {
		t.Errorf("status = %d, want 503", code)
	}
}

// Silently ignoring an undocumented status would let a client believe it had
// tested a path the API cannot actually produce.
func TestForcingAnUndocumentedStatusIsRejected(t *testing.T) {
	op := core.NewOperation("x")
	op.SetResponse(200, core.NewResponse("OK"))
	w := get(t, docWith(nil, "/x", "get", op), http.MethodGet, "/x?__status=418")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ---- CORS ----

// A browser will not send the real request until the preflight passes, and
// being called from a frontend on another origin is the entire use case.
func TestPreflightIsAnswered(t *testing.T) {
	w := get(t, constrainedDoc(), http.MethodOptions, "/things")
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("no CORS header on the preflight")
	}
}

func TestResponsesCarryCORS(t *testing.T) {
	if h := get(t, constrainedDoc(), http.MethodGet, "/things").Header().Get("Access-Control-Allow-Origin"); h != "*" {
		t.Errorf("= %q, want *", h)
	}
}

// ---- termination ----

// A model that refers to itself is ordinary — a category with children, a
// comment with replies — and must not hang the server.
func TestRecursiveSchemaTerminates(t *testing.T) {
	schemas := map[string]*core.Schema{
		"Node": {Type: "object", Properties: map[string]*core.Schema{
			"id":       {Type: "integer"},
			"children": {Type: "array", Items: &core.Schema{Ref: "#/components/schemas/Node"}},
		}},
	}
	doc := docWith(schemas, "/nodes", "get", okOp("Node"))

	done := make(chan int, 1)
	go func() { done <- get(t, doc, http.MethodGet, "/nodes").Code }()
	select {
	case code := <-done:
		if code != 200 {
			t.Errorf("status = %d", code)
		}
	case <-timeout():
		t.Fatal("the mock did not terminate on a recursive schema")
	}
}

// Two schemas that refer to each other never repeat within one branch, so the
// cycle guard alone does not stop them; the depth limit does.
func TestMutuallyRecursiveSchemasTerminate(t *testing.T) {
	schemas := map[string]*core.Schema{
		"A": {Type: "object", Properties: map[string]*core.Schema{"b": {Ref: "#/components/schemas/B"}}},
		"B": {Type: "object", Properties: map[string]*core.Schema{"a": {Ref: "#/components/schemas/A"}}},
	}
	doc := docWith(schemas, "/a", "get", okOp("A"))

	done := make(chan int, 1)
	go func() { done <- get(t, doc, http.MethodGet, "/a").Code }()
	select {
	case code := <-done:
		if code != 200 {
			t.Errorf("status = %d", code)
		}
	case <-timeout():
		t.Fatal("mutually recursive schemas did not terminate")
	}
}

func TestEmptyDocumentServesNothingRatherThanPanicking(t *testing.T) {
	if code := get(t, core.NewDocument("t", "1"), http.MethodGet, "/x").Code; code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
	w := httptest.NewRecorder()
	Handler(nil).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("nil document: status = %d, want 404", w.Code)
	}
}

// A response with no documented body answers with the status alone rather than
// an empty JSON object that a client would try to parse.
func TestBodylessResponseHasNoBody(t *testing.T) {
	op := core.NewOperation("x")
	op.SetResponse(204, core.NewResponse("No Content"))
	w := get(t, docWith(nil, "/x", "delete", op), http.MethodDelete, "/x")
	if w.Code != 204 {
		t.Errorf("status = %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", w.Body.String())
	}
}

// timeout is a helper so the termination tests read the same way.
func timeout() <-chan time.Time { return time.After(5 * time.Second) }
