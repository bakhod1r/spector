// Package route answers the question every consumer of a document eventually
// asks: which documented operation is this request?
//
// It is one implementation on purpose. The mock serves a request by finding the
// operation for it; the proxy judges a live request by finding the operation
// for it. If those two disagreed about whether GET /users/me is /users/{id},
// the proxy would report drift the mock does not have, and the report would be
// about Specter rather than about the API.
package route

import (
	"sort"
	"strings"

	"github.com/bakhod1r/spector/internal/core"
)

// Route is one compiled operation: the path split into segments so matching
// does not re-parse it per request.
type Route struct {
	Method string // upper case
	Path   string // as documented, with placeholders: /users/{id}
	Op     *core.Operation

	segments []string
}

// Compile prepares a document's operations for matching.
//
// The order is what makes matching deterministic: shorter paths first, then
// those with fewer parameters, so /users/me is preferred over /users/{id}
// whatever order the document's maps happen to iterate in. Without it the same
// request would match different operations on different runs.
func Compile(doc *core.Document) []Route {
	var out []Route
	if doc == nil {
		return out
	}
	for path, methods := range doc.Paths {
		for method, op := range methods {
			out = append(out, Route{
				Method:   strings.ToUpper(method),
				Path:     path,
				Op:       op,
				segments: Split(path),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].segments) != len(out[j].segments) {
			return len(out[i].segments) < len(out[j].segments)
		}
		if paramCount(out[i].segments) != paramCount(out[j].segments) {
			return paramCount(out[i].segments) < paramCount(out[j].segments)
		}
		// A total order, so two operations that tie on shape still sort the
		// same way every time.
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// Match finds the operation for a request and extracts the path parameters, so
// a caller can tell what /users/42 said about 42.
func Match(routes []Route, method, path string) (Route, map[string]string, bool) {
	got := Split(path)
	for _, rt := range routes {
		if !strings.EqualFold(rt.Method, method) || len(rt.segments) != len(got) {
			continue
		}
		vals := map[string]string{}
		ok := true
		for i, seg := range rt.segments {
			switch {
			case IsParam(seg):
				vals[strings.Trim(seg, "{}")] = got[i]
			case seg != got[i]:
				ok = false
			}
			if !ok {
				break
			}
		}
		if ok {
			return rt, vals, true
		}
	}
	return Route{}, nil, false
}

// Split breaks a path into its non-empty segments, so a trailing slash or a
// doubled one does not change what a path means.
func Split(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// IsParam reports whether a segment is a placeholder rather than a literal.
func IsParam(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

func paramCount(segs []string) int {
	n := 0
	for _, s := range segs {
		if IsParam(s) {
			n++
		}
	}
	return n
}
