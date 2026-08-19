// Package mock serves a generated document as a working HTTP API.
//
// The point is to unblock a frontend before the backend exists. Specter already
// knows every path, method, status code and response schema, so the mock is not
// a new source of truth — it is the document, executed. That is also its
// limitation, and the honest way to describe it: it returns shaped data, not
// real data. Two GETs of the same resource return the same body; a POST does
// not change what a later GET returns.
//
// Making it stateful would mean guessing at semantics the document does not
// describe, and a mock that is subtly wrong about behaviour is worse than one
// that is obviously only about shape.
package mock

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"github.com/bakhod1r/spector/internal/core"
	"github.com/bakhod1r/spector/internal/route"
)

// Handler serves the document with the default options.
func Handler(doc *core.Document) http.Handler {
	return HandlerWith(doc, DefaultOptions())
}

// HandlerWith serves the document. Requests that match no documented operation
// get a problem document saying so, rather than a bare 404: the most common
// reason to reach a mock and get nothing is a path that is not in the spec, and
// saying which paths are is more useful than silence.
func HandlerWith(doc *core.Document, opts Options) http.Handler {
	routes := route.Compile(doc)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A disallowed origin gets no CORS headers, which is what makes the
		// browser block it. The request is still answered: refusing it here
		// would misrepresent CORS, which is enforced by the browser and not by
		// the server.
		opts.applyCORS(w, r)

		// A browser will not send the real request until the preflight passes,
		// and the whole point of the mock is to be called from a frontend on
		// another origin.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		rt, params, ok := route.Match(routes, r.Method, r.URL.Path)
		if !ok {
			writeProblem(w, http.StatusNotFound,
				"no documented operation matches "+r.Method+" "+r.URL.Path)
			return
		}

		if opts.EnforceAuth {
			if missing, ok := authCheck(doc, rt.Op, r); !ok {
				w.Header().Set("WWW-Authenticate", "Bearer")
				writeProblem(w, http.StatusUnauthorized,
					"missing credentials: "+missing)
				return
			}
		}

		// An explicitly requested status wins, so a client can exercise its
		// error handling without the mock having to guess when to fail.
		status, body := primary(rt.Op)
		if want := r.URL.Query().Get("__status"); want != "" {
			if n, err := strconv.Atoi(want); err == nil {
				if resp, has := rt.Op.Responses[want]; has {
					status, body = n, resp
				} else {
					writeProblem(w, http.StatusBadRequest,
						"status "+want+" is not documented for this operation")
					return
				}
			}
		}

		if body == nil || len(body.Content) == 0 {
			w.WriteHeader(status)
			return
		}
		media, ok := body.Content["application/json"]
		if !ok || media.Schema == nil {
			w.WriteHeader(status)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(Sample(doc, media.Schema, params))
	})
}

// primary picks the response the mock returns by default: the lowest documented
// 2xx, falling back to the lowest status of any kind so an operation that only
// documents failures still answers something.
func primary(op *core.Operation) (int, *core.Response) {
	codes := make([]int, 0, len(op.Responses))
	for code := range op.Responses {
		if n, err := strconv.Atoi(code); err == nil {
			codes = append(codes, n)
		}
	}
	sort.Ints(codes)
	for _, c := range codes {
		if c >= 200 && c < 300 {
			return c, op.Responses[strconv.Itoa(c)]
		}
	}
	if len(codes) > 0 {
		return codes[0], op.Responses[strconv.Itoa(codes[0])]
	}
	return http.StatusOK, nil
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   "about:blank",
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
	})
}
