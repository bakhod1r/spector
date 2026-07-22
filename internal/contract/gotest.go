package contract

import (
	"encoding/json"
	"strconv"

	"github.com/user/specter/internal/core"
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
	})
	if err != nil {
		return nil, err
	}
	return gofmt("check.go", src)
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
