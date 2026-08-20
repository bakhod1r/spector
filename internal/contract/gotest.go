package contract

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/bakhod1r/spector/internal/conform"
	"github.com/bakhod1r/spector/internal/core"
)

// renderGoTest writes the test file: one test per documented endpoint, and
// nothing else. Everything the tests lean on lives in check.go, so this file
// stays readable at fifty endpoints and a reader can see at a glance which
// endpoints are covered.
func renderGoTest(doc *core.Document, reqs []request, opts Options) ([]byte, error) {
	data := map[string]any{
		"Package":    opts.Package,
		"Components": componentsJSON(doc),
		"Requests":   goRequests(reqs),
	}
	src, err := render("contract_test.go.tmpl", data)
	if err != nil {
		return nil, err
	}
	return gofmt("contract_test.go", src)
}

// renderCheck writes the support file. It is generated rather than vendored
// because it carries the base URL and the auth scheme the document declared.
func renderCheck(opts Options, a *auth) ([]byte, error) {
	src, err := render("check.go.tmpl", map[string]any{
		"Package": opts.Package,
		"BaseURL": opts.BaseURL,
		"Auth":    a,
		"Conform": conformBody(),
	})
	if err != nil {
		return nil, err
	}
	return gofmt("check.go", src)
}

// conformBody returns Spector's own shape checker, ready to be pasted into the
// generated package.
//
// The alternative — writing the checks a second time in a template — would let
// the two drift, and then the same response could pass a project's contract
// tests and fail the traffic proxy, with neither result meaning anything. So
// the text is carried instead of the logic being restated, and a test compares
// what comes out with the package it came from.
//
// The package clause and the import block are dropped: the generated file
// supplies its own, and it needs more imports than the checker alone.
func conformBody() string {
	src := conform.Source()

	if i := strings.Index(src, "\npackage conform\n"); i >= 0 {
		src = src[i+len("\npackage conform\n"):]
	}
	// The import block is the first parenthesised group; it ends at the first
	// line that is exactly ")".
	if i := strings.Index(src, "\nimport ("); i >= 0 {
		if j := strings.Index(src[i:], "\n)\n"); j >= 0 {
			src = src[:i] + src[i+j+len("\n)\n"):]
		}
	}
	// The embed of its own source has no meaning outside the package it was
	// written in, and would not compile without the file beside it.
	src = stripEmbed(src)
	return strings.TrimSpace(src)
}

// stripEmbed removes the //go:embed declaration and the Source accessor, which
// exist only so this text can be carried here in the first place.
func stripEmbed(src string) string {
	start := strings.Index(src, "// source is this file's own text.")
	end := strings.Index(src, "func Source() string { return source }")
	if start < 0 || end < 0 {
		return src
	}
	return src[:start] + src[end+len("func Source() string { return source }"):]
}

// goRequest is one request as the template needs it: every value already
// rendered, so the template holds no logic.
type goRequest struct {
	Name          string
	GoName        string
	What          string
	Method        string
	PathWithQuery string
	Headers       []kv
	BodyLiteral   string
	Statuses      []int
	Success       int
	SchemaJSON    string
}

func goRequests(reqs []request) []goRequest {
	out := make([]goRequest, 0, len(reqs))
	seen := map[string]int{}

	for _, r := range reqs {
		name := goName(r.Name)
		// Two operations can reasonably share a summary ("Delete"), and two Go
		// functions cannot share a name.
		seen[name]++
		if n := seen[name]; n > 1 {
			name += strconv.Itoa(n)
		}

		g := goRequest{
			Name:          r.Name,
			GoName:        name,
			What:          r.Method + " " + r.RawPath,
			Method:        r.Method,
			PathWithQuery: r.Path + queryString(r),
			Headers:       r.Headers,
			Statuses:      r.Statuses,
			Success:       r.Success,
		}
		if len(r.Body) > 0 {
			g.BodyLiteral = strconv.Quote(string(r.Body))
		}
		if r.Schema != nil {
			if data, err := json.Marshal(r.Schema); err == nil {
				g.SchemaJSON = string(data)
			}
		}
		out = append(out, g)
	}
	return out
}

// componentsJSON carries the document's component schemas into the generated
// file, so a $ref in a response schema can be followed at run time without the
// document being on disk.
//
// All of them are carried rather than only the reachable ones: the document is
// already pruned to what the routes touch, and taking the whole set keeps the
// generated file from churning when one endpoint's response changes.
func componentsJSON(doc *core.Document) string {
	data, err := json.Marshal(doc.Components.Schemas)
	if err != nil {
		return "{}"
	}
	return string(data)
}
