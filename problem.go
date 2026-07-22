package specter

import (
	"encoding/json"
	"net/http"
)

// ProblemContentType is the media type RFC 9457 defines for these documents.
// It is not application/json: the distinct type is what lets a client tell an
// error document from a successful response without inspecting the body.
const ProblemContentType = "application/problem+json"

// APIError is an RFC 9457 problem document.
//
// Nothing in Specter requires this type. The console will document whatever
// error shape a service already returns, and the standards advice will point at
// RFC 9457 without insisting. This exists so that a service that does want a
// consistent error format does not have to write it out again, and gets a
// document that matches — the same struct produces the response at runtime and
// the schema at generation time, so the two cannot drift.
//
// The fields are exactly the members RFC 9457 defines:
//
//	type      a URI identifying the problem kind; the stable thing a client
//	          branches on. "about:blank" when there is nothing to point at.
//	title     a short, human-readable summary, the same for every occurrence.
//	status    the HTTP status code, repeated here so the body is self-contained
//	          once it has been logged or forwarded away from its response.
//	detail    what went wrong this time. Meant for a human, not for parsing.
//	instance  a URI for this specific occurrence, e.g. a request id.
//
// Extensions carries any additional members. RFC 9457 allows them at the top
// level, and they are how field-level validation errors are usually reported.
type APIError struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`

	// Extensions are merged into the top-level object rather than nested, which
	// is what the RFC specifies. A key that collides with a standard member is
	// dropped, since a document with two meanings for "status" is worse than
	// one missing an extension.
	Extensions map[string]any `json:"-"`
}

// Error makes APIError usable as an ordinary Go error, so a handler can return
// it up the stack rather than writing the response where the failure happened.
func (e *APIError) Error() string {
	if e.Detail != "" {
		return e.Title + ": " + e.Detail
	}
	return e.Title
}

// NewProblem builds a problem document with the title taken from the status
// code's standard reason phrase, which is almost always the right title and is
// one less thing to get inconsistent between handlers.
func NewProblem(status int, detail string) *APIError {
	return &APIError{
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	}
}

// WithType sets the problem type URI and returns the error, so a package can
// define its problem kinds as one-liners.
func (e *APIError) WithType(uri string) *APIError {
	e.Type = uri
	return e
}

// WithInstance sets the occurrence URI, typically a request id.
func (e *APIError) WithInstance(uri string) *APIError {
	e.Instance = uri
	return e
}

// With adds an extension member.
func (e *APIError) With(key string, value any) *APIError {
	if e.Extensions == nil {
		e.Extensions = map[string]any{}
	}
	e.Extensions[key] = value
	return e
}

// standardMembers are the names an extension may not take, because the RFC has
// already given them a meaning.
var standardMembers = map[string]bool{
	"type": true, "title": true, "status": true, "detail": true, "instance": true,
}

// MarshalJSON flattens Extensions into the top-level object, as RFC 9457
// requires. Without this they would either nest under a key of their own or be
// dropped entirely.
func (e APIError) MarshalJSON() ([]byte, error) {
	// The alias sheds the custom marshaller, so encoding the standard members
	// does not recurse into this method.
	type alias APIError
	base, err := json.Marshal(alias(e))
	if err != nil {
		return nil, err
	}
	if len(e.Extensions) == 0 {
		return base, nil
	}

	var merged map[string]any
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	for k, v := range e.Extensions {
		if standardMembers[k] {
			continue
		}
		merged[k] = v
	}
	return json.Marshal(merged)
}

// WriteProblem sends the document with the right status and content type.
//
// It is the whole reason to use this type rather than a bare struct: the
// content type is the part everyone forgets, and without it a client cannot
// distinguish a problem document from any other JSON body.
func WriteProblem(w http.ResponseWriter, e *APIError) {
	if e == nil {
		e = NewProblem(http.StatusInternalServerError, "")
	}
	status := e.Status
	if status == 0 {
		status = http.StatusInternalServerError
		e.Status = status
	}
	w.Header().Set("Content-Type", ProblemContentType)
	w.WriteHeader(status)
	// The status and headers are already written, so a failure here cannot be
	// reported to the client; there is nothing useful left to do with the error.
	_ = json.NewEncoder(w).Encode(e)
}
