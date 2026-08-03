package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/specter/internal/core"
)

// doc is a two-endpoint API: a list of users and one user by id.
func doc() *core.Document {
	d := core.NewDocument("Shop", "1")
	d.Components.Schemas["User"] = &core.Schema{
		Type:     "object",
		Required: []string{"id", "name"},
		Properties: map[string]*core.Schema{
			"id":   {Type: "integer"},
			"name": {Type: "string"},
		},
	}
	userRef := &core.Schema{Ref: "#/components/schemas/User"}

	list := core.NewOperation("listUsers")
	list.Responses = map[string]*core.Response{
		"200": {Description: "ok", Content: map[string]core.MediaType{
			"application/json": {Schema: &core.Schema{Type: "array", Items: userRef}},
		}},
	}
	d.AddOperation("/users", "get", list)

	get := core.NewOperation("getUser")
	get.Responses = map[string]*core.Response{
		"200": {Description: "ok", Content: map[string]core.MediaType{
			"application/json": {Schema: userRef},
		}},
		"404": {Description: "not found"},
	}
	d.AddOperation("/users/{id}", "get", get)

	return d
}

// api stands in for the real service. Each test hands it the behaviour it wants
// to see reported, which is the only way to test a watcher: by watching
// something that misbehaves on purpose.
func api(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func front(t *testing.T, target string, opts Options) (*Proxy, *httptest.Server) {
	t.Helper()
	opts.Target = target
	p, err := New(doc(), opts)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(p.Handler())
	t.Cleanup(s.Close)
	return p, s
}

func call(t *testing.T, s *httptest.Server, method, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, s.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func kinds(p *Proxy) []string {
	var out []string
	for _, f := range p.Findings() {
		out = append(out, f.Kind)
	}
	return out
}

func findingOf(t *testing.T, p *Proxy, kind string) Finding {
	t.Helper()
	for _, f := range p.Findings() {
		if f.Kind == kind {
			return f
		}
	}
	t.Fatalf("no %s finding; got %v", kind, kinds(p))
	return Finding{}
}

// ---- forwarding comes first ----

// The proxy is a watcher, not a gate. Whatever it thinks of a response, the
// client must receive exactly what the API sent.
func TestTrafficIsForwardedUntouched(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom", "kept")
		w.WriteHeader(201)
		io.WriteString(w, `{"id": 1, "name": "ada"}`)
	})
	_, s := front(t, upstream.URL, Options{})

	res := call(t, s, "GET", "/users/1")
	body, _ := io.ReadAll(res.Body)

	if res.StatusCode != 201 {
		t.Errorf("status = %d, want the upstream's 201", res.StatusCode)
	}
	if res.Header.Get("X-Custom") != "kept" {
		t.Error("an upstream header was dropped")
	}
	if strings.TrimSpace(string(body)) != `{"id": 1, "name": "ada"}` {
		t.Errorf("body = %q, want it byte-for-byte", body)
	}
}

// A finding must never cost the client its response.
func TestAResponseTheProxyDislikesStillReachesTheClient(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id": "not-an-integer"}`)
	})
	p, s := front(t, upstream.URL, Options{})

	res := call(t, s, "GET", "/users/1")
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(body), "not-an-integer") {
		t.Errorf("the client did not get the real response: %d %q", res.StatusCode, body)
	}
	if len(p.Findings()) == 0 {
		t.Error("the disagreement was not reported")
	}
}

// An unreachable target is the proxy's failure, and saying so is more useful
// than a bare 502 that reads as the API being down.
func TestUnreachableTargetSaysWhichTargetItIs(t *testing.T) {
	_, s := front(t, "http://127.0.0.1:1", Options{})
	res := call(t, s, "GET", "/users")
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "127.0.0.1:1") {
		t.Errorf("body = %q, want the target named", body)
	}
}

// ---- what it reports ----

func TestAConformingAPIProducesNoFindings(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[{"id": 1, "name": "ada"}]`)
	})
	p, s := front(t, upstream.URL, Options{})

	call(t, s, "GET", "/users")
	if got := p.Findings(); len(got) != 0 {
		t.Errorf("findings = %v, want none for an API that matches its document", got)
	}
	if p.Requests() != 1 {
		t.Errorf("requests = %d, want 1", p.Requests())
	}
}

// The endpoint the scanner never saw: the reason to watch traffic at all.
func TestUndocumentedEndpointIsReported(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	p, s := front(t, upstream.URL, Options{})

	call(t, s, "POST", "/internal/reindex")
	f := findingOf(t, p, KindUndocumentedEndpoint)
	if f.Method != "POST" || f.Path != "/internal/reindex" {
		t.Errorf("finding = %+v, want the request named", f)
	}
}

// A status no client was told about is the one it will not handle.
func TestUndocumentedStatusIsReported(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	p, s := front(t, upstream.URL, Options{})

	call(t, s, "GET", "/users/1")
	f := findingOf(t, p, KindUndocumentedStatus)
	if !strings.Contains(f.Detail, "500") || !strings.Contains(f.Detail, "200, 404") {
		t.Errorf("detail = %q, want both what happened and what was documented", f.Detail)
	}
}

func TestShapeDriftIsReported(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id": "1", "name": "ada"}`)
	})
	p, s := front(t, upstream.URL, Options{})

	call(t, s, "GET", "/users/1")
	f := findingOf(t, p, KindShape)
	if !strings.Contains(f.Detail, "response.id") {
		t.Errorf("detail = %q, want the offending field named", f.Detail)
	}
}

func TestMissingRequiredFieldIsReported(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id": 1}`)
	})
	p, s := front(t, upstream.URL, Options{})

	call(t, s, "GET", "/users/1")
	if f := findingOf(t, p, KindShape); !strings.Contains(f.Detail, "name") {
		t.Errorf("detail = %q, want the missing field named", f.Detail)
	}
}

// HTML where JSON was promised breaks every generated client, however correct
// the status code is.
func TestWrongContentTypeIsReported(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<html>oops</html>")
	})
	p, s := front(t, upstream.URL, Options{})

	call(t, s, "GET", "/users/1")
	if f := findingOf(t, p, KindContentType); !strings.Contains(f.Detail, "text/html") {
		t.Errorf("detail = %q, want the actual type named", f.Detail)
	}
}

// An extra field is reported but is a different kind, because it is a different
// thing: the API grew, nothing broke.
func TestAnExtraFieldIsItsOwnKind(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id": 1, "name": "ada", "nickname": "a"}`)
	})
	p, s := front(t, upstream.URL, Options{})

	call(t, s, "GET", "/users/1")
	for _, f := range p.Findings() {
		if f.Kind == KindShape {
			t.Errorf("an extra field was reported as a violation: %s", f.Detail)
		}
	}
	if f := findingOf(t, p, KindUndocumentedField); !strings.Contains(f.Detail, "nickname") {
		t.Errorf("detail = %q", f.Detail)
	}
}

// A documented endpoint with no documented body has nothing to contradict.
func TestNoBodyDocumentedMeansNothingToCheck(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		io.WriteString(w, `{"whatever": true}`)
	})
	p, s := front(t, upstream.URL, Options{})

	call(t, s, "GET", "/users/1")
	if got := p.Findings(); len(got) != 0 {
		t.Errorf("findings = %v, want none: the 404 documents no body", got)
	}
}

// ---- aggregation ----

// The same drift on a busy endpoint is one finding with a count, not a hundred
// lines. A report nobody can read is a report nobody reads.
func TestRepeatsAggregateAndAnnounceOnce(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id": "1", "name": "ada"}`)
	})
	announced := 0
	p, s := front(t, upstream.URL, Options{OnFinding: func(Finding) { announced++ }})

	for i := 0; i < 5; i++ {
		call(t, s, "GET", "/users/1")
	}

	got := p.Findings()
	if len(got) != 1 {
		t.Fatalf("findings = %v, want one aggregated finding", got)
	}
	if got[0].Count != 5 {
		t.Errorf("count = %d, want 5", got[0].Count)
	}
	if announced != 1 {
		t.Errorf("announced %d times, want once", announced)
	}
}

// Different ids are the same endpoint, so they aggregate — but the report keeps
// one real path, because a template is not something you can curl.
func TestDifferentIdsAggregateAndKeepAnExample(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id": "x"}`)
	})
	p, s := front(t, upstream.URL, Options{})

	call(t, s, "GET", "/users/7")
	call(t, s, "GET", "/users/9")

	f := findingOf(t, p, KindShape)
	if f.Path != "/users/{id}" {
		t.Errorf("path = %q, want the documented template", f.Path)
	}
	if f.First != "/users/7" {
		t.Errorf("firstSeen = %q, want the first real path", f.First)
	}
}

// The report is diffed between CI runs, so the same traffic must produce the
// same bytes.
func TestReportOrderIsStable(t *testing.T) {
	upstream := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
	})
	p, s := front(t, upstream.URL, Options{})
	for i := 0; i < 3; i++ {
		call(t, s, "GET", "/users/1")
	}
	call(t, s, "GET", "/nope")

	first, err := json.Marshal(p.Report(upstream.URL))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		next, _ := json.Marshal(p.Report(upstream.URL))
		if string(first) != string(next) {
			t.Fatal("the report changed between renderings")
		}
	}
	// Most frequent first: three 500s outrank one unknown path.
	if p.Findings()[0].Kind != KindUndocumentedStatus {
		t.Errorf("first = %s, want the most frequent finding", p.Findings()[0].Kind)
	}
}

// ---- configuration ----

func TestATargetIsRequired(t *testing.T) {
	if _, err := New(doc(), Options{}); err == nil {
		t.Error("expected an error with no target")
	}
	if _, err := New(doc(), Options{Target: "localhost:3000"}); err == nil {
		t.Error("expected an error for a target with no scheme")
	}
}
