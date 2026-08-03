package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func learned(t *testing.T, exchanges ...Exchange) map[string]map[string]any {
	t.Helper()
	l := NewLearner()
	for _, ex := range exchanges {
		l.Observe(ex, "")
	}
	data, err := json.Marshal(l.Document("Shop", "1"))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out.Paths
}

// Every id being its own endpoint would make the fragment a list of thousands
// of paths instead of one, which is the difference between a finding and noise.
func TestIdentifierSegmentsCollapse(t *testing.T) {
	cases := map[string]string{
		"/users/1":           "/users/{id}",
		"/users/42/orders/7": "/users/{id}/orders/{id}",
		"/things/3f2504e0-4f89-11d3-9a0c-0305e82c3301": "/things/{id}",
		"/things/a1b2c3d4e5f60718":                     "/things/{id}",
		"/posts/hello-world":                           "/posts/hello-world",
		"/users/me":                                    "/users/me",
	}
	for in, want := range cases {
		if got := NormalisePath(in); got != want {
			t.Errorf("NormalisePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// A slug is not an id. Collapsing it would claim an endpoint the API may not
// have, and a fragment that invents endpoints is worse than one that misses
// them.
func TestSlugsAreLeftAlone(t *testing.T) {
	if got := NormalisePath("/posts/my-first-post/comments"); got != "/posts/my-first-post/comments" {
		t.Errorf("= %q, want the slug kept", got)
	}
}

func TestObservedEndpointsBecomeOperations(t *testing.T) {
	paths := learned(t,
		Exchange{Method: "GET", Path: "/internal/health", Status: 200},
		Exchange{Method: "POST", Path: "/internal/reindex", Status: 202},
	)
	if _, ok := paths["/internal/health"]["get"]; !ok {
		t.Errorf("paths = %v, want the observed GET", paths)
	}
	if _, ok := paths["/internal/reindex"]["post"]; !ok {
		t.Errorf("paths = %v, want the observed POST", paths)
	}
}

// Repeats of the same endpoint are one operation with every status it answered.
func TestStatusesAccumulatePerEndpoint(t *testing.T) {
	paths := learned(t,
		Exchange{Method: "GET", Path: "/internal/health", Status: 200},
		Exchange{Method: "GET", Path: "/internal/health", Status: 200},
		Exchange{Method: "GET", Path: "/internal/health", Status: 503},
	)
	op := paths["/internal/health"]["get"].(map[string]any)
	responses := op["responses"].(map[string]any)
	if len(responses) != 2 {
		t.Fatalf("responses = %v, want 200 and 503", responses)
	}
	if !strings.Contains(responses["200"].(map[string]any)["description"].(string), "2 time") {
		t.Errorf("the 200 does not say how often it was seen: %v", responses["200"])
	}
}

func TestResponseSchemaIsInferredFromTheBody(t *testing.T) {
	paths := learned(t, Exchange{
		Method: "GET", Path: "/internal/stats", Status: 200,
		ResponseBody: []byte(`{"count": 3, "ratio": 0.5, "ok": true, "name": "x", "tags": ["a"]}`),
	})
	op := paths["/internal/stats"]["get"].(map[string]any)
	schema := op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	props := schema["properties"].(map[string]any)

	want := map[string]string{"count": "integer", "ratio": "number", "ok": "boolean", "name": "string", "tags": "array"}
	for name, typ := range want {
		got := props[name].(map[string]any)["type"]
		if got != typ {
			t.Errorf("%s: type = %v, want %v", name, got, typ)
		}
	}
}

// A path parameter invented by normalisation has to be declared, or the
// fragment is not a valid document.
func TestCollapsedSegmentsAreDeclaredAsParameters(t *testing.T) {
	paths := learned(t, Exchange{Method: "GET", Path: "/internal/jobs/42", Status: 200})
	op := paths["/internal/jobs/{id}"]["get"].(map[string]any)
	params, ok := op["parameters"].([]any)
	if !ok || len(params) != 1 {
		t.Fatalf("parameters = %v, want the id declared", op["parameters"])
	}
	if params[0].(map[string]any)["name"] != "id" {
		t.Errorf("parameter = %v, want id", params[0])
	}
}

// The fragment is evidence, not a specification, and has to say so where a
// reader will see it before merging anything.
func TestTheFragmentSaysItIsInferred(t *testing.T) {
	l := NewLearner()
	l.Observe(Exchange{Method: "GET", Path: "/internal/health", Status: 200}, "")
	doc := l.Document("Shop", "1")

	if !strings.Contains(doc.Info.Title, "observed") {
		t.Errorf("title = %q, want it marked as observed", doc.Info.Title)
	}
	op := doc.Paths["/internal/health"]["get"]
	if !strings.Contains(op.Description, "review") {
		t.Errorf("description = %q, want it to ask for review", op.Description)
	}
}

func TestEmptyLearnerHasNothingToWrite(t *testing.T) {
	if !NewLearner().Empty() {
		t.Error("a learner that saw nothing is not empty")
	}
	var nilLearner *Learner
	nilLearner.Observe(Exchange{}, "")
	if !nilLearner.Empty() {
		t.Error("a nil learner is not empty")
	}
}

// The proxy only hands the learner what the document does not already cover;
// learning what is already written down would produce a fragment of noise.
func TestProxyOnlyLearnsWhatIsMissing(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/users" {
			io.WriteString(w, `[{"id":1,"name":"ada"}]`)
			return
		}
		io.WriteString(w, `{"ok":true}`)
	})
	l := NewLearner()
	_, s := front(t, upstream.URL, Options{Learner: l})

	call(t, s, "GET", "/users")           // documented and conforming
	call(t, s, "GET", "/internal/health") // not documented at all

	doc := l.Document("Shop", "1")
	if _, present := doc.Paths["/users"]; present {
		t.Error("an endpoint the document already has was learned")
	}
	if _, present := doc.Paths["/internal/health"]; !present {
		t.Errorf("the undocumented endpoint was not learned: %v", doc.Paths)
	}
}
