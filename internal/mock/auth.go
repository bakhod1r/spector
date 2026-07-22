package mock

import (
	"net/http"
	"strings"

	"github.com/user/specter/internal/core"
)

// authCheck decides whether a request satisfies an operation's documented
// security. It checks presence, not validity: the mock has no way to know what
// a real token looks like, so any bearer token passes and an empty one does
// not. That is exactly the failure mode worth catching before the backend
// exists — a frontend that forgets to attach the header at all.
//
// The requirements to enforce are the operation's own, falling back to the
// document's global security. Entries in the list are alternatives, matching
// the OpenAPI semantics: satisfying any one of them is enough.
func authCheck(doc *core.Document, op *core.Operation, r *http.Request) (missing string, ok bool) {
	reqs := op.Security
	if len(reqs) == 0 {
		reqs = doc.Security
	}
	if len(reqs) == 0 {
		return "", true // undocumented means unprotected; the mock does not invent rules
	}

	var wants []string
	for _, req := range reqs {
		satisfied := len(req) > 0
		for name := range req {
			scheme := doc.Components.SecuritySchemes[name]
			if scheme == nil {
				// A requirement naming an undefined scheme cannot be checked;
				// treating it as satisfied would silently disable enforcement,
				// so it counts as unsatisfied and shows up in the 401 detail.
				satisfied = false
				wants = append(wants, name)
				continue
			}
			if !schemeSatisfied(scheme, r) {
				satisfied = false
				wants = append(wants, describeScheme(name, scheme))
			}
		}
		if satisfied {
			return "", true
		}
	}
	return strings.Join(wants, ", or "), false
}

func schemeSatisfied(s *core.SecurityScheme, r *http.Request) bool {
	switch s.Type {
	case "http":
		auth := r.Header.Get("Authorization")
		switch s.Scheme {
		case "basic":
			return strings.HasPrefix(auth, "Basic ") && len(auth) > len("Basic ")
		default: // bearer
			return strings.HasPrefix(auth, "Bearer ") && len(auth) > len("Bearer ")
		}
	case "apiKey":
		switch s.In {
		case "query":
			return r.URL.Query().Get(s.Name) != ""
		case "cookie":
			c, err := r.Cookie(s.Name)
			return err == nil && c.Value != ""
		default: // header
			return r.Header.Get(s.Name) != ""
		}
	}
	// An unknown scheme type cannot be checked; passing it would be a silent
	// lie, failing it would block schemes the model does not cover. Presence of
	// any Authorization header is the least wrong reading.
	return r.Header.Get("Authorization") != ""
}

func describeScheme(name string, s *core.SecurityScheme) string {
	switch {
	case s.Type == "http" && s.Scheme == "basic":
		return name + " (Authorization: Basic ...)"
	case s.Type == "http":
		return name + " (Authorization: Bearer ...)"
	case s.Type == "apiKey":
		in := s.In
		if in == "" {
			in = "header"
		}
		return name + " (" + s.Name + " in " + in + ")"
	}
	return name
}
