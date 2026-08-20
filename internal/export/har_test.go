package export

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

func TestHARFullDocument(t *testing.T) {
	data, err := HAR(sampleDoc())
	if err != nil {
		t.Fatal(err)
	}
	var log harFile
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatal(err)
	}
	if log.Log.Version != "1.2" {
		t.Errorf("version = %q", log.Log.Version)
	}
	if log.Log.Creator.Name != "spector" {
		t.Errorf("creator = %+v", log.Log.Creator)
	}
	// One entry per operation: GET /users/{id}, POST /users, GET /health.
	if len(log.Log.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(log.Log.Entries))
	}

	byURL := map[string]harEntry{}
	for _, e := range log.Log.Entries {
		byURL[e.Request.Method+" "+e.Request.URL] = e
	}
	// URLs are absolute against the first server (HAR has no variables).
	get, ok := byURL["GET https://api.example.com/users/{id}"]
	if !ok {
		t.Fatalf("missing GET entry; have %v", keysOf(byURL))
	}
	// The documented 200 seeds the response body.
	if get.Response.Status != 200 || !strings.Contains(get.Response.Content.Text, "id") {
		t.Errorf("response = %+v", get.Response)
	}

	post := byURL["POST https://api.example.com/users"]
	if post.Request.PostData == nil || !strings.Contains(post.Request.PostData.Text, "name") {
		t.Errorf("postData = %+v", post.Request.PostData)
	}
	// Header params and the JSON content-type land in request headers.
	var names []string
	for _, h := range post.Request.Headers {
		names = append(names, h.Name)
	}
	if !contains(names, "X-Request-Id") || !contains(names, "Content-Type") {
		t.Errorf("headers = %v", names)
	}
	// Optional query params carry through the query string.
	if len(post.Request.QueryString) != 1 || post.Request.QueryString[0].Name != "dry_run" {
		t.Errorf("query = %+v", post.Request.QueryString)
	}
}

// With no server the URLs fall back to a relative path rather than an invalid
// absolute one.
func TestHARNoServer(t *testing.T) {
	data, err := HAR(&core.Document{Paths: map[string]map[string]*core.Operation{
		"/x": {"get": {}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var log harFile
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatal(err)
	}
	if got := log.Log.Entries[0].Request.URL; got != "/x" {
		t.Errorf("url = %q, want relative /x", got)
	}
}

func TestHAREmptyDocument(t *testing.T) {
	data, err := HAR(&core.Document{})
	if err != nil {
		t.Fatal(err)
	}
	var log harFile
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatal(err)
	}
	if len(log.Log.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(log.Log.Entries))
	}
}

func TestHARDeterministic(t *testing.T) {
	for i := 0; i < 5; i++ {
		a, _ := HAR(sampleDoc())
		b, _ := HAR(sampleDoc())
		if string(a) != string(b) {
			t.Fatal("HAR output is not stable")
		}
	}
}

func keysOf(m map[string]harEntry) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
