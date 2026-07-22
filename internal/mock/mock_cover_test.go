package mock

import (
	"net/http"
	"testing"

	"github.com/user/specter/internal/core"
)

// ---- sample edge cases ----

func TestSampleNilSchema(t *testing.T) {
	if got := Sample(core.NewDocument("t", "1"), nil, nil); got != nil {
		t.Errorf("= %v, want nil", got)
	}
}

func TestSampleUnresolvableRef(t *testing.T) {
	doc := core.NewDocument("t", "1")
	got := Sample(doc, &core.Schema{Ref: "#/components/schemas/Ghost"}, nil)
	if m, ok := got.(map[string]any); !ok || len(m) != 0 {
		t.Errorf("= %v, want an empty object for an unknown ref", got)
	}
	if resolve(nil, "X") != nil {
		t.Error("resolve on a nil document must be nil")
	}
}

func TestSampleAllOfMerges(t *testing.T) {
	doc := core.NewDocument("t", "1")
	schema := &core.Schema{
		AllOf: []*core.Schema{
			{Type: "object", Properties: map[string]*core.Schema{"a": {Type: "string"}}},
			{Type: "string"}, // a non-object member contributes nothing
		},
		Properties: map[string]*core.Schema{"b": {Type: "integer"}},
	}
	got, ok := Sample(doc, schema, nil).(map[string]any)
	if !ok {
		t.Fatalf("= %T, want an object", got)
	}
	if got["a"] != "string" || got["b"] != 1 {
		t.Errorf("= %v, want both members merged", got)
	}
}

func TestSampleArrayMaxItemsZero(t *testing.T) {
	got := Sample(core.NewDocument("t", "1"), &core.Schema{
		Type: "array", MaxItems: ptrI(0), Items: &core.Schema{Type: "string"},
	}, nil)
	if arr, ok := got.([]any); !ok || len(arr) != 0 {
		t.Errorf("= %v, want an empty array", got)
	}
}

func TestSampleUnknownTypeIsNull(t *testing.T) {
	if got := Sample(core.NewDocument("t", "1"), &core.Schema{Type: "mystery"}, nil); got != nil {
		t.Errorf("= %v, want nil", got)
	}
}

func TestNumberInRangeExclusiveBounds(t *testing.T) {
	got := numberInRange(&core.Schema{
		Minimum: ptrF(5), ExclusiveMinimum: true,
		Maximum: ptrF(4), ExclusiveMaximum: true,
	}, 1)
	// min pushes to 6, exclusive max pulls the cap to 3, and the clamp wins.
	if got != 3 {
		t.Errorf("= %v, want 3", got)
	}
}

func TestCoerce(t *testing.T) {
	if got := coerce(nil, "42"); got != "42" {
		t.Errorf("nil schema: = %v, want the raw text", got)
	}
	if got := coerce(&core.Schema{Type: "integer"}, "-7"); got != -7 {
		t.Errorf("negative int: = %v, want -7", got)
	}
	if got := coerce(&core.Schema{Type: "number"}, "1.5"); got != "1.5" {
		t.Errorf("number stays text: = %v", got)
	}
	if got := coerce(&core.Schema{Type: "string"}, "abc"); got != "abc" {
		t.Errorf("string: = %v", got)
	}
}

// ---- handler edge cases ----

// A response documented only as text/plain has no JSON to fabricate.
func TestNonJSONContentAnswersBare(t *testing.T) {
	op := core.NewOperation("x")
	op.SetResponse(200, &core.Response{
		Description: "ok",
		Content:     map[string]core.MediaType{"text/plain": {Schema: &core.Schema{Type: "string"}}},
	})
	doc := docWith(nil, "/plain", "get", op)
	w := get(t, doc, http.MethodGet, "/plain")
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Errorf("= %d %q, want a bare 200", w.Code, w.Body.String())
	}
}

// An operation that only documents failures still answers with something.
func TestErrorOnlyOperationAnswersItsLowestStatus(t *testing.T) {
	op := core.NewOperation("x")
	op.SetResponse(500, core.NewResponse("boom", core.WithJSONBody(&core.Schema{Type: "object"})))
	doc := docWith(nil, "/broken", "get", op)
	if w := get(t, doc, http.MethodGet, "/broken"); w.Code != 500 {
		t.Errorf("= %d, want 500", w.Code)
	}
}

// No numeric status at all falls back to 200 with no body.
func TestNonNumericResponsesFallBackTo200(t *testing.T) {
	op := core.NewOperation("x")
	op.Responses = map[string]*core.Response{"default": {Description: "d"}}
	doc := docWith(nil, "/default", "get", op)
	if w := get(t, doc, http.MethodGet, "/default"); w.Code != 200 {
		t.Errorf("= %d, want 200", w.Code)
	}
}

// Routes of different depths force the length branch of the sort.
func TestRoutesOfDifferentDepthsSort(t *testing.T) {
	doc := core.NewDocument("t", "1")
	for _, p := range []string{"/a/b/c", "/a", "/a/b"} {
		op := core.NewOperation("x")
		op.SetResponse(204, &core.Response{Description: "no content"})
		doc.AddOperation(p, "get", op)
	}
	rs := compile(doc)
	for i := 1; i < len(rs); i++ {
		if len(rs[i-1].segments) > len(rs[i].segments) {
			t.Fatalf("not sorted by depth: %v then %v", rs[i-1].segments, rs[i].segments)
		}
	}
}

// ---- cors ----

// Credentials plus wildcard with no Origin header: not a CORS request, allowed,
// no origin echoed.
func TestCredentialsWildcardWithoutOrigin(t *testing.T) {
	o := Options{AllowCredentials: true}
	value, allowed := o.originFor("")
	if !allowed || value != "" {
		t.Errorf("= %q, %v; want empty and allowed", value, allowed)
	}
}
