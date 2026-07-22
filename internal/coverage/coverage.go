// Package coverage measures how documented an API actually is.
//
// A generated document always covers every route — that is what generation
// means — but coverage here asks a stricter question: how much of what a
// consumer needs is present? A route with no summary, no typed response, or
// no error responses appears in the document without really being documented.
package coverage

import (
	"fmt"
	"sort"
	"strings"

	"github.com/user/specter/internal/core"
)

// Check is one aspect an operation can be missing.
type Check struct {
	Name string // short id: "summary", "response-type", ...
	Desc string
}

// The checks, in report order. Each is something a consumer of the API notices
// when absent, which is the bar for counting it against coverage.
var checks = []Check{
	{"summary", "has a summary (doc comment on the handler)"},
	{"response-type", "has at least one response with a typed body"},
	{"request-type", "body-carrying method documents its request type"},
	{"error-response", "documents at least one non-2xx response"},
	{"tags", "grouped with tags"},
}

// Gap is one missing aspect on one operation.
type Gap struct {
	Method string
	Path   string
	Check  string
	Source *core.Source // where the handler lives, when known
}

// Report is the result of measuring a document.
type Report struct {
	Operations int
	// PerCheck maps a check name to how many operations pass it.
	PerCheck map[string]int
	Gaps     []Gap

	// applicable counts, per check, the operations the check applies to —
	// the denominator PerCheck is measured against.
	applicable map[string]int
}

// Percent is overall coverage: passed checks over applicable checks, in
// percent. Applicable matters — request-type is not counted against a GET,
// which has no body to document.
func (r Report) Percent() float64 {
	applicable := 0
	passed := 0
	for _, c := range checks {
		applicable += r.applicable[c.Name]
		passed += r.PerCheck[c.Name]
	}
	if applicable == 0 {
		return 100
	}
	return 100 * float64(passed) / float64(applicable)
}

// Measure runs every check against every operation.
func Measure(doc *core.Document) Report {
	r := Report{PerCheck: map[string]int{}, applicable: map[string]int{}}

	for _, path := range sortedPaths(doc) {
		for _, method := range sortedMethods(doc.Paths[path]) {
			op := doc.Paths[path][method]
			r.Operations++
			for _, c := range checks {
				ok, applies := evaluate(c.Name, method, op)
				if !applies {
					continue
				}
				r.applicable[c.Name]++
				if ok {
					r.PerCheck[c.Name]++
				} else {
					r.Gaps = append(r.Gaps, Gap{
						Method: strings.ToUpper(method),
						Path:   path,
						Check:  c.Name,
						Source: op.Source,
					})
				}
			}
		}
	}
	return r
}

func evaluate(check, method string, op *core.Operation) (ok, applies bool) {
	switch check {
	case "summary":
		return op.Summary != "", true
	case "response-type":
		for _, resp := range op.Responses {
			for _, media := range resp.Content {
				if media.Schema != nil {
					return true, true
				}
			}
		}
		return false, true
	case "request-type":
		switch strings.ToUpper(method) {
		case "POST", "PUT", "PATCH":
			return op.RequestBody != nil, true
		}
		return false, false // no body expected; the check does not apply
	case "error-response":
		for code := range op.Responses {
			if len(code) > 0 && code[0] != '2' {
				return true, true
			}
		}
		return false, true
	case "tags":
		return len(op.Tags) > 0, true
	}
	return false, false
}

// Render writes the report as text: overall percent, per-check counts, then
// every gap with its source location so the fix is one click away.
func (r Report) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "coverage: %.1f%% (%d operations)\n\n", r.Percent(), r.Operations)
	for _, c := range checks {
		total := r.applicable[c.Name]
		if total == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %-15s %3d/%-3d %s\n", c.Name, r.PerCheck[c.Name], total, c.Desc)
	}
	if len(r.Gaps) > 0 {
		b.WriteString("\ngaps:\n")
		for _, g := range r.Gaps {
			loc := ""
			if g.Source != nil {
				loc = fmt.Sprintf("  (%s:%d)", g.Source.File, g.Source.Line)
			}
			fmt.Fprintf(&b, "  %s %s: missing %s%s\n", g.Method, g.Path, g.Check, loc)
		}
	}
	return b.String()
}

func sortedPaths(doc *core.Document) []string {
	out := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

var methodOrder = map[string]int{
	"get": 0, "post": 1, "put": 2, "patch": 3, "delete": 4, "head": 5, "options": 6,
}

func sortedMethods(ops map[string]*core.Operation) []string {
	out := make([]string, 0, len(ops))
	for m := range ops {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return methodOrder[out[i]] < methodOrder[out[j]] })
	return out
}
