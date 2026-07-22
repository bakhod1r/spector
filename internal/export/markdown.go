package export

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/mock"
)

// Markdown renders the document as a static API reference suitable for a
// README or a docs site. It is generated for reading, not for machines: the
// schema details that matter to a person — fields, types, which are required,
// example bodies — are shown inline, and the OpenAPI bookkeeping is not.
func Markdown(doc *core.Document) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", doc.Info.Title)
	if doc.Info.Version != "" {
		fmt.Fprintf(&b, "Version: `%s`\n\n", doc.Info.Version)
	}
	if len(doc.Servers) > 0 {
		b.WriteString("## Servers\n\n")
		for _, s := range doc.Servers {
			if s.Description != "" {
				fmt.Fprintf(&b, "- `%s` — %s\n", s.URL, s.Description)
			} else {
				fmt.Fprintf(&b, "- `%s`\n", s.URL)
			}
		}
		b.WriteString("\n")
	}

	writeAuth(&b, doc)

	// Group by tag (falling back to first path segment) so the reference reads
	// as resources rather than one flat wall of endpoints.
	groups := map[string][]string{} // group -> "method path" keys
	keys := map[string]*core.Operation{}
	var order []string
	for _, path := range sortedPaths(doc) {
		for _, method := range sortedMethods(doc.Paths[path]) {
			op := doc.Paths[path][method]
			g := groupOf(op, path)
			if _, seen := groups[g]; !seen {
				order = append(order, g)
			}
			key := strings.ToUpper(method) + " " + path
			groups[g] = append(groups[g], key)
			keys[key] = op
		}
	}

	for _, g := range order {
		fmt.Fprintf(&b, "## %s\n\n", g)
		for _, key := range groups[g] {
			writeOperation(&b, doc, key, keys[key])
		}
	}

	return []byte(b.String())
}

func writeAuth(b *strings.Builder, doc *core.Document) {
	if len(doc.Components.SecuritySchemes) == 0 {
		return
	}
	b.WriteString("## Authentication\n\n")
	names := make([]string, 0, len(doc.Components.SecuritySchemes))
	for n := range doc.Components.SecuritySchemes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := doc.Components.SecuritySchemes[n]
		switch {
		case s.Type == "http" && s.Scheme == "bearer":
			fmt.Fprintf(b, "- **%s** — bearer token in `Authorization: Bearer <token>`", n)
			if s.BearerFormat != "" {
				fmt.Fprintf(b, " (%s)", s.BearerFormat)
			}
			b.WriteString("\n")
		case s.Type == "http" && s.Scheme == "basic":
			fmt.Fprintf(b, "- **%s** — HTTP basic auth\n", n)
		case s.Type == "apiKey":
			fmt.Fprintf(b, "- **%s** — API key `%s` in %s\n", n, s.Name, s.In)
		default:
			fmt.Fprintf(b, "- **%s** — %s\n", n, s.Type)
		}
	}
	b.WriteString("\n")
}

func writeOperation(b *strings.Builder, doc *core.Document, key string, op *core.Operation) {
	fmt.Fprintf(b, "### `%s`\n\n", key)
	if op.Deprecated {
		b.WriteString("> **Deprecated.**\n\n")
	}
	if op.Summary != "" {
		fmt.Fprintf(b, "%s\n\n", op.Summary)
	}
	if op.Description != "" {
		fmt.Fprintf(b, "%s\n\n", op.Description)
	}
	if len(op.Security) > 0 {
		var names []string
		for _, req := range op.Security {
			for n := range req {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		fmt.Fprintf(b, "Auth required: %s\n\n", strings.Join(names, ", "))
	}

	var params []core.Parameter
	for _, p := range op.Parameters {
		if rp := resolveParam(doc, p); rp.Name != "" {
			params = append(params, rp)
		}
	}
	if len(params) > 0 {
		b.WriteString("| Parameter | In | Type | Required | Description |\n|---|---|---|---|---|\n")
		for _, p := range params {
			typ := ""
			if p.Schema != nil {
				typ = p.Schema.Type
			}
			req := ""
			if p.Required {
				req = "yes"
			}
			fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s |\n",
				p.Name, p.In, typ, req, strings.ReplaceAll(p.Description, "\n", " "))
		}
		b.WriteString("\n")
	}

	if op.RequestBody != nil {
		if media, ok := op.RequestBody.Content["application/json"]; ok && media.Schema != nil {
			b.WriteString("Request body:\n\n")
			writeExample(b, doc, media.Schema)
		}
	}

	var codes []string
	for code := range op.Responses {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		resp := op.Responses[code]
		fmt.Fprintf(b, "Response `%s`", code)
		if resp.Description != "" {
			fmt.Fprintf(b, " — %s", resp.Description)
		}
		b.WriteString(":\n\n")
		if media, ok := resp.Content["application/json"]; ok && media.Schema != nil {
			writeExample(b, doc, media.Schema)
		}
	}
}

// writeExample shows a sampled body rather than a schema table: an example is
// what a reader copies into their client, and it carries the field names and
// shapes anyway.
func writeExample(b *strings.Builder, doc *core.Document, schema *core.Schema) {
	data, err := json.MarshalIndent(mock.Sample(doc, schema, nil), "", "  ")
	if err != nil {
		return
	}
	fmt.Fprintf(b, "```json\n%s\n```\n\n", data)
}
