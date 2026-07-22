// Package sdk generates client code from an OpenAPI document.
//
// The output is source the caller owns, in the same spirit as the admin
// generator: a typed method per operation and a struct/interface per schema,
// with no runtime dependency beyond the language's standard HTTP client. It is
// a starting point that compiles, not a framework to configure.
package sdk

import (
	"fmt"
	"sort"
	"strings"

	"github.com/user/specter/internal/core"
)

// Options selects the output language and names the generated artifact.
type Options struct {
	// Lang is "ts" or "go".
	Lang string
	// Package names the generated Go package. Ignored for TypeScript.
	// Empty means "client".
	Package string
	// BaseURL is the default server the client calls. Empty falls back to the
	// document's first server, and failing that the client requires one at
	// construction.
	BaseURL string
}

// File is one generated file, named relative to the output directory.
type File struct {
	Name string
	Data []byte
}

// Generate renders the document as client source in the requested language.
func Generate(doc *core.Document, opts Options) ([]File, error) {
	if opts.BaseURL == "" && len(doc.Servers) > 0 {
		opts.BaseURL = doc.Servers[0].URL
	}
	switch opts.Lang {
	case "ts", "typescript":
		return generateTS(doc, opts)
	case "go", "golang":
		if opts.Package == "" {
			opts.Package = "client"
		}
		return generateGo(doc, opts)
	default:
		return nil, fmt.Errorf("sdk: unknown language %q (want ts or go)", opts.Lang)
	}
}

// operation is one flattened endpoint, ready to render.
type operation struct {
	Name       string // method name in the generated client
	Method     string // upper-case HTTP method
	Path       string // /users/{id}
	PathParams []string
	Query      []core.Parameter
	Body       *core.Schema // request body schema, nil when there is none
	Result     *core.Schema // 2xx response schema, nil when bodiless
	Summary    string
	Deprecated bool
}

// flatten walks the document in a deterministic order and names every
// operation. operationId wins when present; otherwise the name is derived from
// the method and path, so GET /api/v1/users/{id} becomes getApiV1UsersById.
func flatten(doc *core.Document) []operation {
	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths) // map order is random; generated code must not churn

	seen := map[string]int{}
	var ops []operation
	for _, path := range paths {
		methods := make([]string, 0, len(doc.Paths[path]))
		for m := range doc.Paths[path] {
			methods = append(methods, m)
		}
		sort.Strings(methods)

		for _, method := range methods {
			op := doc.Paths[path][method]
			if op == nil {
				continue
			}
			o := operation{
				Method:     strings.ToUpper(method),
				Path:       path,
				Summary:    op.Summary,
				Deprecated: op.Deprecated,
			}
			for _, p := range op.Parameters {
				switch p.In {
				case "path":
					o.PathParams = append(o.PathParams, p.Name)
				case "query":
					o.Query = append(o.Query, p)
				}
			}
			if op.RequestBody != nil {
				if mt, ok := op.RequestBody.Content["application/json"]; ok {
					o.Body = mt.Schema
				}
			}
			o.Result = successSchema(op)

			name := op.OperationID
			if name == "" {
				name = deriveName(method, path)
			}
			// Two operations may derive the same name; a numeric suffix keeps
			// the output compiling rather than failing on a duplicate.
			seen[name]++
			if n := seen[name]; n > 1 {
				name = fmt.Sprintf("%s%d", name, n)
			}
			o.Name = name
			ops = append(ops, o)
		}
	}
	return ops
}

// successSchema picks the first 2xx response that carries a JSON body.
func successSchema(op *core.Operation) *core.Schema {
	codes := make([]string, 0, len(op.Responses))
	for c := range op.Responses {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		if !strings.HasPrefix(c, "2") {
			continue
		}
		r := op.Responses[c]
		if r == nil {
			continue
		}
		if mt, ok := r.Content["application/json"]; ok && mt.Schema != nil {
			return mt.Schema
		}
	}
	return nil
}

// deriveName turns "get" + "/api/v1/users/{id}" into getApiV1UsersById.
func deriveName(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, "{") {
			b.WriteString("By")
			seg = strings.Trim(seg, "{}")
		}
		b.WriteString(exportName(seg))
	}
	return b.String()
}

// exportName makes an identifier fragment: "users" → "Users", "user-id" →
// "UserId". Anything that is not a letter or digit splits words.
func exportName(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			if up {
				b.WriteRune(r &^ 0x20) // upper-case
				up = false
			} else {
				b.WriteRune(r)
			}
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			up = true
		default:
			up = true
		}
	}
	return b.String()
}

// refName pulls "User" out of "#/components/schemas/User".
func refName(ref string) string {
	i := strings.LastIndex(ref, "/")
	return ref[i+1:]
}

// sortedSchemaNames keeps generated type declarations stable across runs.
func sortedSchemaNames(doc *core.Document) []string {
	names := make([]string, 0, len(doc.Components.Schemas))
	for n := range doc.Components.Schemas {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
