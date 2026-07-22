package advice

import (
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

func op(responses map[string]*core.Response) *core.Operation {
	return &core.Operation{Responses: responses}
}

func jsonResp(contentType string, props ...string) *core.Response {
	schema := &core.Schema{Type: "object", Properties: map[string]*core.Schema{}}
	for _, p := range props {
		schema.Properties[p] = &core.Schema{Type: "string"}
	}
	return &core.Response{
		Description: "err",
		Content:     map[string]core.MediaType{contentType: {Schema: schema}},
	}
}

func rules(as []Advice) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Rule)
	}
	return out
}

func hasRule(as []Advice, rule string) bool {
	for _, a := range as {
		if a.Rule == rule {
			return true
		}
	}
	return false
}

// ---- RFC 9457 ----

func TestErrorWithPlainJSONIsFlagged(t *testing.T) {
	got := reviewOperation("get", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"404": jsonResp(plainJSON, "error"),
	}))
	if !hasRule(got, "rfc9457-content-type") {
		t.Errorf("= %v, want the content-type advice", rules(got))
	}
}

// An API that already conforms must be left alone. A linter that fires on
// correct code trains people to ignore it.
func TestConformingErrorIsNotFlagged(t *testing.T) {
	got := reviewOperation("get", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"404": jsonResp(problemJSON, "type", "title", "status", "detail", "instance"),
	}))
	if len(got) != 0 {
		t.Errorf("= %v, want nothing", rules(got))
	}
}

// A body that already has most of the problem members is clearly aiming at
// RFC 9457, so naming the gaps helps. One that shares nothing with it is a
// different shape entirely, and the content-type advice already covers that.
func TestPartialProblemDocumentNamesTheGaps(t *testing.T) {
	got := reviewOperation("get", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"400": jsonResp(problemJSON, "title", "status", "detail"),
	}))
	if !hasRule(got, "rfc9457-fields") {
		t.Fatalf("= %v, want the fields advice", rules(got))
	}
	for _, a := range got {
		if a.Rule == "rfc9457-fields" {
			if !strings.Contains(a.Message, "type") || !strings.Contains(a.Message, "instance") {
				t.Errorf("message does not name the missing members: %s", a.Message)
			}
		}
	}
}

func TestUnrelatedShapeIsNotToldToAddFiveFields(t *testing.T) {
	got := reviewOperation("get", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"500": jsonResp(problemJSON, "code", "reason"),
	}))
	if hasRule(got, "rfc9457-fields") {
		t.Errorf("= %v, want no field-by-field advice on an unrelated shape", rules(got))
	}
}

// Five error codes with the same wrong content type are one problem, not five.
func TestAdviceIsNotRepeatedPerStatusCode(t *testing.T) {
	got := reviewOperation("get", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"400": jsonResp(plainJSON, "error"),
		"401": jsonResp(plainJSON, "error"),
		"403": jsonResp(plainJSON, "error"),
		"404": jsonResp(plainJSON, "error"),
		"500": jsonResp(plainJSON, "error"),
	}))
	n := 0
	for _, r := range rules(got) {
		if r == "rfc9457-content-type" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("content-type advice appears %d times, want 1", n)
	}
}

func TestMissingErrorResponseIsFlagged(t *testing.T) {
	got := reviewOperation("get", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
	}))
	if !hasRule(got, "no-error-response") {
		t.Errorf("= %v, want the missing-error advice", rules(got))
	}
}

// An operation with no responses at all is not an operation with undocumented
// errors; there is nothing to say about it.
func TestEmptyOperationGetsNoAdvice(t *testing.T) {
	if got := reviewOperation("get", "/x", op(map[string]*core.Response{})); len(got) != 0 {
		t.Errorf("= %v, want nothing", rules(got))
	}
}

// ---- RFC 9110 ----

func TestPostReturning200IsAdvisedAbout201(t *testing.T) {
	got := reviewOperation("post", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"400": jsonResp(problemJSON, "type", "title", "status", "detail", "instance"),
	}))
	if !hasRule(got, "post-created") {
		t.Errorf("= %v, want the 201 advice", rules(got))
	}
}

func TestPostAlreadyReturning201IsLeftAlone(t *testing.T) {
	got := reviewOperation("post", "/x", op(map[string]*core.Response{
		"201": {Description: "created"},
		"400": jsonResp(problemJSON, "type", "title", "status", "detail", "instance"),
	}))
	if hasRule(got, "post-created") {
		t.Errorf("= %v, want no 201 advice", rules(got))
	}
}

func TestDeleteWithEmpty200IsAdvisedAbout204(t *testing.T) {
	got := reviewOperation("delete", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"404": jsonResp(problemJSON, "type", "title", "status", "detail", "instance"),
	}))
	if !hasRule(got, "delete-no-content") {
		t.Errorf("= %v, want the 204 advice", rules(got))
	}
}

// A DELETE that returns a body has a reason to use 200.
func TestDeleteWithABodyIsLeftAlone(t *testing.T) {
	got := reviewOperation("delete", "/x", op(map[string]*core.Response{
		"200": jsonResp(plainJSON, "deleted"),
		"404": jsonResp(problemJSON, "type", "title", "status", "detail", "instance"),
	}))
	if hasRule(got, "delete-no-content") {
		t.Errorf("= %v, want no 204 advice", rules(got))
	}
}

// The method decides which rules apply.
func TestGetIsNotAdvisedAboutCreatedOrNoContent(t *testing.T) {
	got := reviewOperation("get", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"404": jsonResp(problemJSON, "type", "title", "status", "detail", "instance"),
	}))
	for _, r := range []string{"post-created", "delete-no-content"} {
		if hasRule(got, r) {
			t.Errorf("%s applied to a GET: %v", r, rules(got))
		}
	}
}

// ---- shape of the output ----

// A recommendation without a citation is an assertion. Every one has to say
// where it comes from so a reader can disagree with the standard rather than
// with the tool.
func TestEveryAdviceCitesAStandard(t *testing.T) {
	got := reviewOperation("post", "/x", op(map[string]*core.Response{
		"200": {Description: "ok"},
		"400": jsonResp(plainJSON, "error"),
	}))
	if len(got) == 0 {
		t.Fatal("no advice produced for a fixture that should produce some")
	}
	for _, a := range got {
		if a.Reference == "" || !strings.HasPrefix(a.Reference, "RFC") {
			t.Errorf("%s cites %q", a.Rule, a.Reference)
		}
		if a.Message == "" || a.Rule == "" {
			t.Errorf("incomplete advice: %+v", a)
		}
		if a.Severity != Should && a.Severity != Consider {
			t.Errorf("%s has severity %q", a.Rule, a.Severity)
		}
	}
}

func TestOutputIsStable(t *testing.T) {
	build := func() []string {
		return rules(reviewOperation("post", "/x", op(map[string]*core.Response{
			"200": {Description: "ok"},
			"400": jsonResp(plainJSON, "error"),
			"500": jsonResp(plainJSON, "error"),
		})))
	}
	first := build()
	for i := 0; i < 5; i++ {
		got := build()
		if len(got) != len(first) {
			t.Fatalf("run %d: %v vs %v", i, got, first)
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d differs: %v vs %v", i, got, first)
			}
		}
	}
}

func TestReviewKeysAreMethodAndPath(t *testing.T) {
	doc := core.NewDocument("t", "1")
	doc.AddOperation("/users", "get", op(map[string]*core.Response{"200": {Description: "ok"}}))

	got := Review(doc)
	if _, ok := got["GET /users"]; !ok {
		t.Errorf("keys = %v, want GET /users", keys(got))
	}
}

func TestReviewOnNilDocument(t *testing.T) {
	if got := Review(nil); got != nil {
		t.Errorf("= %v, want nil", got)
	}
}

func keys(m map[string][]Advice) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
