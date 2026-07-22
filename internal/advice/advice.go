// Package advice reviews a generated document against HTTP and JSON standards
// and reports where an API diverges from them.
//
// These are recommendations, never rewrites. Specter documents what the code
// does; if it silently reshaped an error body to match RFC 9457 the document
// would stop describing the service and start describing an aspiration. So the
// findings are attached to the document and shown in the console, and changing
// the code stays the developer's decision.
//
// Every rule cites the document that motivates it, because "your errors should
// look like this" is worth nothing without a reason, and a reader who disagrees
// deserves to be able to go and check.
package advice

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/user/specter/internal/core"
)

// Advice is one recommendation. It is an alias of the document model's type so
// findings can be attached to an operation without conversion.
type Advice = core.Advisory

// Severities. Nothing here is an error: a document that ignores every one of
// these is still a valid OpenAPI document describing a working API.
const (
	// Should marks a documented requirement of a standard the API already
	// claims to follow (it speaks HTTP and JSON).
	Should = "should"
	// Consider marks a convention that is widely useful but genuinely optional.
	Consider = "consider"
)

const (
	problemJSON = "application/problem+json"
	plainJSON   = "application/json"

	refRFC9457 = "RFC 9457 (Problem Details for HTTP APIs)"
	refRFC9110 = "RFC 9110 (HTTP Semantics)"
)

// Review returns advice for a whole document, keyed by "METHOD path" so the
// console can attach each list to its operation.
func Review(doc *core.Document) map[string][]Advice {
	if doc == nil {
		return nil
	}
	out := map[string][]Advice{}
	for path, methods := range doc.Paths {
		for method, op := range methods {
			if as := reviewOperation(method, path, op); len(as) > 0 {
				out[strings.ToUpper(method)+" "+path] = as
			}
		}
	}
	return out
}

// reviewOperation runs every rule over one operation. Results are sorted by
// rule id so the output is stable between runs.
func reviewOperation(method, path string, op *core.Operation) []Advice {
	if op == nil {
		return nil
	}
	var out []Advice
	out = append(out, errorResponseAdvice(op)...)
	out = append(out, statusCodeAdvice(method, op)...)

	sort.Slice(out, func(i, j int) bool { return out[i].Rule < out[j].Rule })
	return out
}

// errorResponseAdvice covers RFC 9457, which standardises what an HTTP error
// body looks like. Before it, every API invented its own shape — {"error":"..."},
// {"message":...}, {"errors":[...]} — and every client wrote a bespoke parser.
func errorResponseAdvice(op *core.Operation) []Advice {
	var out []Advice
	var errorCodes []string

	for code, resp := range op.Responses {
		n, err := strconv.Atoi(code)
		if err != nil || n < 400 || n > 599 {
			continue
		}
		errorCodes = append(errorCodes, code)

		if resp == nil || len(resp.Content) == 0 {
			continue // an error with no body is a separate matter, below
		}
		if _, ok := resp.Content[problemJSON]; ok {
			continue // already conforms
		}
		if _, ok := resp.Content[plainJSON]; ok {
			out = append(out, Advice{
				Rule:     "rfc9457-content-type",
				Severity: Should,
				Message: fmt.Sprintf(
					"%s returns application/json; error responses should use %s so clients can recognise a problem document without guessing at the shape",
					code, problemJSON),
				Reference: refRFC9457,
			})
		}
	}

	sort.Strings(errorCodes)

	// An operation that documents no failure at all is the more common problem,
	// and the more expensive one: a client written against the document has
	// nothing to handle.
	if len(errorCodes) == 0 && len(op.Responses) > 0 {
		out = append(out, Advice{
			Rule:      "no-error-response",
			Severity:  Should,
			Message:   "no failure response is documented; a client written from this document has nothing to handle when the call fails",
			Reference: refRFC9110,
		})
		return out
	}

	// Shape check: a problem document has type/title/status/detail/instance.
	// title and status carry the most weight — they are what a generic client
	// displays and branches on.
	for _, code := range errorCodes {
		resp := op.Responses[code]
		if resp == nil {
			continue
		}
		media, ok := resp.Content[problemJSON]
		if !ok {
			if media, ok = resp.Content[plainJSON]; !ok {
				continue
			}
		}
		missing := missingProblemFields(media.Schema)
		if len(missing) > 0 && len(missing) < 5 {
			// Fewer than all five: the body is clearly trying to be a problem
			// document, so naming the gaps is useful. All five missing means it
			// is some other shape entirely, which the content-type rule covers.
			out = append(out, Advice{
				Rule:     "rfc9457-fields",
				Severity: Consider,
				Message: fmt.Sprintf("%s body is close to a problem document but omits %s",
					code, strings.Join(missing, ", ")),
				Reference: refRFC9457,
			})
		}
	}
	return dedupe(out)
}

// problemFields are the members RFC 9457 defines. All are optional in the RFC,
// which is why the rule that names them is "consider" rather than "should".
var problemFields = []string{"type", "title", "status", "detail", "instance"}

func missingProblemFields(schema *core.Schema) []string {
	if schema == nil || schema.Properties == nil {
		return nil
	}
	var missing []string
	for _, f := range problemFields {
		if _, ok := schema.Properties[f]; !ok {
			missing = append(missing, f)
		}
	}
	return missing
}

// statusCodeAdvice covers the status-code semantics RFC 9110 defines, where
// picking the wrong code changes what caches and clients are permitted to do.
func statusCodeAdvice(method string, op *core.Operation) []Advice {
	var out []Advice
	method = strings.ToUpper(method)

	if method == "POST" {
		if _, has201 := op.Responses["201"]; !has201 {
			if _, has200 := op.Responses["200"]; has200 {
				out = append(out, Advice{
					Rule:      "post-created",
					Severity:  Consider,
					Message:   "POST returns 200; if it creates a resource, 201 Created with a Location header tells the client where it went",
					Reference: refRFC9110,
				})
			}
		}
	}

	if method == "DELETE" {
		if resp, ok := op.Responses["200"]; ok && len(resp.Content) == 0 {
			out = append(out, Advice{
				Rule:      "delete-no-content",
				Severity:  Consider,
				Message:   "DELETE returns 200 with no body; 204 No Content says the same thing without implying one is coming",
				Reference: refRFC9110,
			})
		}
	}
	return out
}

// dedupe keeps one advice per rule. A path with five error codes that all use
// the wrong content type has one problem, not five.
func dedupe(in []Advice) []Advice {
	seen := map[string]bool{}
	var out []Advice
	for _, a := range in {
		if seen[a.Rule] {
			continue
		}
		seen[a.Rule] = true
		out = append(out, a)
	}
	return out
}
