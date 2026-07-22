package coverage

import (
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

// op is a small builder so each case shows only the aspect it is about.
func op(fn func(*core.Operation)) *core.Operation {
	o := &core.Operation{Responses: map[string]*core.Response{}}
	if fn != nil {
		fn(o)
	}
	return o
}

func jsonResp(desc string) *core.Response {
	return &core.Response{
		Description: desc,
		Content:     map[string]core.MediaType{"application/json": {Schema: &core.Schema{Type: "object"}}},
	}
}

// An empty document has nothing to measure; Percent must not divide by zero,
// and a vacuous 100% is the honest answer.
func TestMeasureEmptyDocument(t *testing.T) {
	r := Measure(&core.Document{})
	if r.Operations != 0 {
		t.Errorf("operations = %d, want 0", r.Operations)
	}
	if got := r.Percent(); got != 100 {
		t.Errorf("percent = %v, want 100", got)
	}
	if len(r.Gaps) != 0 {
		t.Errorf("gaps = %+v, want none", r.Gaps)
	}
	// Nothing applies, so Render prints no per-check lines and no gaps.
	out := r.Render()
	if !strings.HasPrefix(out, "coverage: 100.0% (0 operations)") {
		t.Errorf("render = %q", out)
	}
	if strings.Contains(out, "gaps:") {
		t.Errorf("render should have no gaps section: %q", out)
	}
}

// A fully documented POST passes every check, including request-type.
func TestMeasureFullyDocumented(t *testing.T) {
	doc := &core.Document{Paths: map[string]map[string]*core.Operation{
		"/users": {"post": op(func(o *core.Operation) {
			o.Summary = "Create a user"
			o.Tags = []string{"users"}
			o.RequestBody = &core.RequestBody{Content: map[string]core.MediaType{
				"application/json": {Schema: &core.Schema{Type: "object"}},
			}}
			o.Responses["201"] = jsonResp("created")
			o.Responses["400"] = &core.Response{Description: "bad request"}
		})},
	}}
	r := Measure(doc)
	if r.Operations != 1 {
		t.Fatalf("operations = %d, want 1", r.Operations)
	}
	if len(r.Gaps) != 0 {
		t.Errorf("gaps = %+v, want none", r.Gaps)
	}
	for _, c := range checks {
		if r.PerCheck[c.Name] != 1 || r.applicable[c.Name] != 1 {
			t.Errorf("check %s = %d/%d, want 1/1", c.Name, r.PerCheck[c.Name], r.applicable[c.Name])
		}
	}
	if got := r.Percent(); got != 100 {
		t.Errorf("percent = %v, want 100", got)
	}
}

// A bare GET fails everything that applies to it, and request-type must not be
// counted at all — a GET has no body to document.
func TestMeasureBareGet(t *testing.T) {
	doc := &core.Document{Paths: map[string]map[string]*core.Operation{
		"/users": {"get": op(nil)},
	}}
	r := Measure(doc)
	if n := r.applicable["request-type"]; n != 0 {
		t.Errorf("request-type applicable = %d, want 0 for GET", n)
	}
	if len(r.Gaps) != 4 {
		t.Fatalf("gaps = %d (%+v), want 4", len(r.Gaps), r.Gaps)
	}
	if got := r.Percent(); got != 0 {
		t.Errorf("percent = %v, want 0", got)
	}
	for _, g := range r.Gaps {
		if g.Method != "GET" || g.Path != "/users" {
			t.Errorf("gap = %+v", g)
		}
		if g.Check == "request-type" {
			t.Errorf("request-type must not be a gap on GET")
		}
	}
}

// Responses that exist but carry no schema still fail response-type: a body
// nobody typed is not documented.
func TestEvaluateResponseTypeBranches(t *testing.T) {
	cases := []struct {
		name string
		op   *core.Operation
		want bool
	}{
		{"no responses", op(nil), false},
		{"response without content", op(func(o *core.Operation) {
			o.Responses["200"] = &core.Response{Description: "ok"}
		}), false},
		{"content with nil schema", op(func(o *core.Operation) {
			o.Responses["200"] = &core.Response{Content: map[string]core.MediaType{"application/json": {}}}
		}), false},
		{"typed content", op(func(o *core.Operation) { o.Responses["200"] = jsonResp("ok") }), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, applies := evaluate("response-type", "get", tc.op)
			if !applies {
				t.Fatal("response-type must always apply")
			}
			if ok != tc.want {
				t.Errorf("ok = %v, want %v", ok, tc.want)
			}
		})
	}
}

// error-response looks at the first digit of the code, so 2xx alone fails and
// any non-2xx passes. An empty code string must not panic on code[0].
func TestEvaluateErrorResponse(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"200", false},
		{"204", false},
		{"404", true},
		{"500", true},
		{"", false},
	}
	for _, tc := range cases {
		t.Run("code="+tc.code, func(t *testing.T) {
			o := op(func(o *core.Operation) { o.Responses[tc.code] = &core.Response{} })
			ok, _ := evaluate("error-response", "get", o)
			if ok != tc.want {
				t.Errorf("ok = %v, want %v", ok, tc.want)
			}
		})
	}
}

// request-type applies only to body-carrying methods, and the method is
// upper-cased first so a lowercase "post" from the parser still counts.
func TestEvaluateRequestTypeApplicability(t *testing.T) {
	for _, m := range []string{"post", "PUT", "patch"} {
		if _, applies := evaluate("request-type", m, op(nil)); !applies {
			t.Errorf("request-type should apply to %s", m)
		}
	}
	for _, m := range []string{"get", "delete", "head", "options"} {
		if _, applies := evaluate("request-type", m, op(nil)); applies {
			t.Errorf("request-type should not apply to %s", m)
		}
	}
	withBody := op(func(o *core.Operation) { o.RequestBody = &core.RequestBody{} })
	if ok, _ := evaluate("request-type", "post", withBody); !ok {
		t.Error("POST with a request body should pass request-type")
	}
}

// An unknown check name is inert rather than fatal.
func TestEvaluateUnknownCheck(t *testing.T) {
	ok, applies := evaluate("nonsense", "get", op(nil))
	if ok || applies {
		t.Errorf("unknown check = (%v, %v), want (false, false)", ok, applies)
	}
}

// summary and tags are the simple presence checks.
func TestEvaluateSummaryAndTags(t *testing.T) {
	if ok, _ := evaluate("summary", "get", op(func(o *core.Operation) { o.Summary = "x" })); !ok {
		t.Error("summary present should pass")
	}
	if ok, _ := evaluate("summary", "get", op(nil)); ok {
		t.Error("missing summary should fail")
	}
	if ok, _ := evaluate("tags", "get", op(func(o *core.Operation) { o.Tags = []string{"t"} })); !ok {
		t.Error("tags present should pass")
	}
	if ok, _ := evaluate("tags", "get", op(nil)); ok {
		t.Error("missing tags should fail")
	}
}

// Gaps come out in path order, then in reader order by method (GET before
// POST), so two runs of the same document produce the same report.
func TestMeasureDeterministicOrder(t *testing.T) {
	doc := &core.Document{Paths: map[string]map[string]*core.Operation{
		"/zebra": {"get": op(nil)},
		"/alpha": {"delete": op(nil), "post": op(nil), "get": op(nil)},
	}}
	first := Measure(doc)
	var order []string
	for _, g := range first.Gaps {
		order = append(order, g.Method+" "+g.Path)
	}
	if order[0] != "GET /alpha" {
		t.Errorf("first gap = %q, want GET /alpha", order[0])
	}
	if order[len(order)-1] != "GET /zebra" {
		t.Errorf("last gap = %q, want GET /zebra", order[len(order)-1])
	}
	// POST must precede DELETE on the same path.
	iPost, iDelete := -1, -1
	for i, k := range order {
		if k == "POST /alpha" && iPost < 0 {
			iPost = i
		}
		if k == "DELETE /alpha" && iDelete < 0 {
			iDelete = i
		}
	}
	if iPost < 0 || iDelete < 0 || iPost > iDelete {
		t.Errorf("method order wrong: %v", order)
	}

	second := Measure(doc)
	if first.Render() != second.Render() {
		t.Error("two measurements of the same document differ")
	}
}

// Render prints the source location when one is known, so the gap is one click
// from the handler, and prints nothing extra when it is not.
func TestRenderGapSource(t *testing.T) {
	doc := &core.Document{Paths: map[string]map[string]*core.Operation{
		"/a": {"get": op(func(o *core.Operation) {
			o.Source = &core.Source{File: "handlers.go", Line: 42}
		})},
		"/b": {"get": op(nil)},
	}}
	out := Measure(doc).Render()
	if !strings.Contains(out, "(handlers.go:42)") {
		t.Errorf("render missing source location:\n%s", out)
	}
	if !strings.Contains(out, "GET /b: missing summary\n") {
		t.Errorf("render should print a sourceless gap plainly:\n%s", out)
	}
	if !strings.Contains(out, "gaps:") {
		t.Errorf("render missing gaps section:\n%s", out)
	}
	// A GET means request-type never applies, so its line is skipped entirely.
	if strings.Contains(out, "request-type") {
		t.Errorf("inapplicable check should not be printed:\n%s", out)
	}
	if !strings.Contains(out, "summary") {
		t.Errorf("applicable check should be printed:\n%s", out)
	}
}

// Percent is passed over applicable, not over operations times checks.
func TestPercentPartial(t *testing.T) {
	doc := &core.Document{Paths: map[string]map[string]*core.Operation{
		"/a": {"get": op(func(o *core.Operation) {
			o.Summary = "s"
			o.Tags = []string{"t"}
			o.Responses["200"] = jsonResp("ok")
		})},
	}}
	// 3 of 4 applicable checks pass (error-response is missing).
	if got := Measure(doc).Percent(); got != 75 {
		t.Errorf("percent = %v, want 75", got)
	}
}
