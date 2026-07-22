package specter

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return m
}

func TestNewProblemTakesTheTitleFromTheStatus(t *testing.T) {
	e := NewProblem(http.StatusNotFound, "no such cart")
	if e.Title != "Not Found" {
		t.Errorf("title = %q, want Not Found", e.Title)
	}
	if e.Status != 404 || e.Detail != "no such cart" {
		t.Errorf("= %+v", e)
	}
}

// RFC 9457 puts extension members at the top level, not nested under a key.
// Nesting them would produce a document that validates as JSON but not as a
// problem document.
func TestExtensionsAreFlattenedToTheTopLevel(t *testing.T) {
	e := NewProblem(422, "validation failed").
		With("errors", map[string]any{"email": "required"})

	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, b)

	if _, nested := m["Extensions"]; nested {
		t.Error("Extensions appears as its own key")
	}
	if _, ok := m["errors"]; !ok {
		t.Errorf("extension is missing from the top level: %s", b)
	}
	if m["status"] != float64(422) {
		t.Errorf("status = %v", m["status"])
	}
}

// An extension that overwrote a standard member would give the document two
// meanings for one name.
func TestExtensionCannotOverwriteAStandardMember(t *testing.T) {
	e := NewProblem(500, "boom").With("status", 200).With("title", "fine")
	m := decode(t, mustMarshal(t, e))

	if m["status"] != float64(500) {
		t.Errorf("status = %v, want the real 500", m["status"])
	}
	if m["title"] != "Internal Server Error" {
		t.Errorf("title = %v, want the real title", m["title"])
	}
}

func TestMarshalWithoutExtensionsIsPlain(t *testing.T) {
	m := decode(t, mustMarshal(t, NewProblem(400, "bad")))
	for _, k := range []string{"title", "status", "detail"} {
		if _, ok := m[k]; !ok {
			t.Errorf("%q missing", k)
		}
	}
	// Empty optional members must not appear as empty strings.
	if _, ok := m["type"]; ok {
		t.Error("empty type was emitted")
	}
	if _, ok := m["instance"]; ok {
		t.Error("empty instance was emitted")
	}
}

// The content type is the part everyone forgets, and it is what lets a client
// recognise a problem document at all.
func TestWriteProblemSetsTheContentType(t *testing.T) {
	w := httptest.NewRecorder()
	WriteProblem(w, NewProblem(404, "gone"))

	if ct := w.Header().Get("Content-Type"); ct != ProblemContentType {
		t.Errorf("Content-Type = %q, want %q", ct, ProblemContentType)
	}
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if decode(t, w.Body.Bytes())["detail"] != "gone" {
		t.Errorf("body = %s", w.Body.String())
	}
}

// A zero status would otherwise be written as HTTP 200, turning a failure into
// a success for every client that checks the code.
func TestWriteProblemDefaultsAMissingStatus(t *testing.T) {
	w := httptest.NewRecorder()
	WriteProblem(w, &APIError{Title: "something went wrong"})

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if decode(t, w.Body.Bytes())["status"] != float64(500) {
		t.Errorf("body does not carry the status: %s", w.Body.String())
	}
}

func TestWriteProblemHandlesNil(t *testing.T) {
	w := httptest.NewRecorder()
	WriteProblem(w, nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// Being an error means a handler can return it up the stack instead of writing
// the response wherever the failure happened.
func TestAPIErrorIsAnError(t *testing.T) {
	var err error = NewProblem(404, "no such cart")
	if err.Error() != "Not Found: no such cart" {
		t.Errorf("= %q", err.Error())
	}
	if NewProblem(404, "").Error() != "Not Found" {
		t.Errorf("= %q", NewProblem(404, "").Error())
	}

	var target *APIError
	if !errors.As(err, &target) {
		t.Error("errors.As cannot recover the problem document")
	}
	if target.Status != 404 {
		t.Errorf("recovered status = %d", target.Status)
	}
}

func TestBuilders(t *testing.T) {
	e := NewProblem(403, "nope").
		WithType("https://example.com/probs/forbidden").
		WithInstance("/requests/abc123")

	if e.Type != "https://example.com/probs/forbidden" || e.Instance != "/requests/abc123" {
		t.Errorf("= %+v", e)
	}
}

// The point of shipping the type is that the response and the documented schema
// come from one declaration and cannot drift apart.
func TestTheTypeDocumentsAsAProblemDocument(t *testing.T) {
	m := decode(t, mustMarshal(t, NewProblem(400, "d").WithType("about:blank").WithInstance("/i")))
	for _, member := range []string{"type", "title", "status", "detail", "instance"} {
		if _, ok := m[member]; !ok {
			t.Errorf("RFC 9457 member %q is missing from the encoded document", member)
		}
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
