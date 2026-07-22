// Package contract turns a document into artefacts that exercise the API it
// describes.
//
// A generated document is a claim, and nothing in Specter until now could check
// it. The console's Execute button answers one request at a time; it is not
// something a repository keeps, a reviewer reads, or CI fails on. So a document
// and the service it describes drift apart silently, which is the one failure
// mode that makes documentation worse than none: it is believed.
//
// Three artefacts come out, from one plan:
//
//   - requests.http — every endpoint as a runnable request, for the editor.
//   - contract_test.go — the same requests as Go tests, for CI.
//   - smoke.sh — status codes only, for a pipeline with nothing installed.
//
// All three are source, not a runtime. That follows the same reasoning as the
// generated admin panel: the first version is free and every version after it
// belongs to the project, which is where the judgement about a particular API
// actually lives.
package contract

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"go/format"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/mock"
)

//go:embed templates/*.tmpl
var assets embed.FS

// Options configures generation.
type Options struct {
	// BaseURL is the API the artefacts call. Empty takes the document's first
	// server, and failing that localhost — a generated artefact should run
	// somewhere rather than not compile.
	BaseURL string
	// Package is the generated Go test package name; "contract" by default.
	Package string
	// Formats selects what is written: "http", "go", "curl". Empty means all
	// three.
	Formats []string
}

// File is one generated file, named relative to the output directory.
type File struct {
	Name string
	Data []byte
}

// DefaultBaseURL is where the artefacts point when neither the caller nor the
// document says otherwise.
const DefaultBaseURL = "http://localhost:8080"

// Generate turns a document into contract artefacts.
func Generate(doc *core.Document, opts Options) ([]File, error) {
	if doc == nil || len(doc.Paths) == 0 {
		return nil, fmt.Errorf("no operations found: there is nothing to exercise, and artefacts that check nothing would pass for the wrong reason")
	}

	formats, err := resolveFormats(opts.Formats)
	if err != nil {
		return nil, err
	}
	if opts.Package == "" {
		opts.Package = "contract"
	}
	if opts.BaseURL == "" {
		if len(doc.Servers) > 0 {
			opts.BaseURL = doc.Servers[0].URL
		} else {
			opts.BaseURL = DefaultBaseURL
		}
	}
	opts.BaseURL = strings.TrimSuffix(opts.BaseURL, "/")

	reqs := requests(doc, opts)
	auth := authOf(doc)

	var files []File
	for _, f := range formats {
		switch f {
		case "http":
			files = append(files, File{"requests.http", renderHTTP(reqs, opts, auth)})
		case "go":
			src, err := renderGoTest(doc, reqs, opts)
			if err != nil {
				return nil, err
			}
			check, err := renderCheck(opts, auth)
			if err != nil {
				return nil, err
			}
			files = append(files, File{"contract_test.go", src}, File{"check.go", check})
		case "curl":
			files = append(files, File{"smoke.sh", renderCurl(reqs, opts, auth)})
		}
	}
	return files, nil
}

// resolveFormats validates the requested formats and puts them in a fixed
// order, so the file list does not depend on how the flag was typed.
func resolveFormats(requested []string) ([]string, error) {
	all := []string{"http", "go", "curl"}
	if len(requested) == 0 {
		return all, nil
	}
	want := map[string]bool{}
	for _, f := range requested {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		known := false
		for _, a := range all {
			if a == f {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown format %q: expected http, go, or curl", f)
		}
		want[f] = true
	}
	if len(want) == 0 {
		return all, nil
	}
	var out []string
	for _, a := range all {
		if want[a] {
			out = append(out, a)
		}
	}
	return out, nil
}

// kv is one parameter, already rendered as the string that goes on the wire.
type kv struct{ Key, Value string }

// request is one call, planned once and rendered three ways. Everything that
// takes judgement — which parameters to send, what a path parameter should be,
// what body satisfies the schema — is decided here, so the three artefacts
// cannot disagree about what they are testing.
type request struct {
	// Name identifies the request to a human and, slugged, to Go.
	Name    string
	Method  string
	Path    string // parameters substituted: /users/1
	RawPath string // as documented: /users/{id}
	Query   []kv
	Headers []kv
	Body    []byte // JSON request body, nil when none is documented

	// Statuses are every documented response code, in order. All of them are
	// legitimate answers: a test that demanded 200 would fail on the 404 the
	// document itself promises.
	Statuses []int
	// Success is the first documented 2xx, and Schema the body it carries.
	// Nil means the response has no documented body to check.
	Success int
	Schema  *core.Schema
}

// requests plans one call per documented operation.
//
// Order is imposed rather than inherited: Go map iteration is random, and an
// artefact whose lines move on every regeneration is unreviewable.
func requests(doc *core.Document, opts Options) []request {
	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var out []request
	for _, path := range paths {
		ops := doc.Paths[path]
		methods := make([]string, 0, len(ops))
		for m := range ops {
			methods = append(methods, m)
		}
		sort.Strings(methods)

		for _, method := range methods {
			out = append(out, plan(doc, path, method, ops[method]))
		}
	}
	return out
}

func plan(doc *core.Document, path, method string, op *core.Operation) request {
	r := request{
		Method:  strings.ToUpper(method),
		RawPath: path,
		Path:    path,
	}
	r.Name = nameOf(op, r.Method, path)

	for _, p := range resolveParams(doc, op.Parameters) {
		switch p.In {
		case "path":
			// A request against /users/{id} literally is a 404 at best, so the
			// placeholder is replaced with something the schema allows.
			r.Path = strings.ReplaceAll(r.Path, "{"+p.Name+"}", pathValue(doc, p.Schema))
		case "query":
			// Only what the API refuses without. A guessed optional parameter
			// makes the call fail for a reason that is not the contract.
			if p.Required {
				r.Query = append(r.Query, kv{p.Name, literal(doc, p.Schema)})
			}
		case "header":
			if p.Required {
				r.Headers = append(r.Headers, kv{p.Name, literal(doc, p.Schema)})
			}
		}
	}

	if op.RequestBody != nil {
		if media, ok := op.RequestBody.Content["application/json"]; ok && media.Schema != nil {
			if body, err := json.MarshalIndent(mock.Sample(doc, media.Schema, nil), "", "  "); err == nil {
				r.Body = body
			}
		}
	}

	for _, code := range sortedStatuses(op.Responses) {
		r.Statuses = append(r.Statuses, code)
		if r.Success == 0 && code >= 200 && code < 300 {
			r.Success = code
			if media, ok := op.Responses[strconv.Itoa(code)].Content["application/json"]; ok {
				r.Schema = media.Schema
			}
		}
	}
	return r
}

// resolveParams follows $refs into components/parameters, where shared headers
// are defined once and referenced from every operation that requires them.
func resolveParams(doc *core.Document, params []core.Parameter) []core.Parameter {
	out := make([]core.Parameter, 0, len(params))
	for _, p := range params {
		if p.Ref == "" {
			out = append(out, p)
			continue
		}
		name := strings.TrimPrefix(p.Ref, "#/components/parameters/")
		if def, ok := doc.Components.Parameters[name]; ok && def != nil {
			out = append(out, *def)
		}
	}
	return out
}

func sortedStatuses(responses map[string]*core.Response) []int {
	var out []int
	for code := range responses {
		n, err := strconv.Atoi(code)
		if err != nil {
			// "default" and the like have no code to compare against.
			continue
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// pathValue renders a path parameter.
//
// A path parameter is an identifier, and an adapter that cannot see a type
// documents it as a plain string — which sampled the ordinary way gives
// /users/string. That is a request no API answers and no reader believes, so an
// unconstrained string becomes "1" instead: it satisfies the schema just as
// well and looks like the thing it stands for.
//
// Anything the document actually constrains — an enum, a format, a length — is
// left to the sampler, because there the document is saying what the endpoint
// accepts and overriding it would send a value the API rejects for a reason
// that has nothing to do with the contract.
func pathValue(doc *core.Document, schema *core.Schema) string {
	if schema != nil && schema.Type == "string" && schema.Ref == "" &&
		len(schema.Enum) == 0 && schema.Format == "" &&
		schema.MinLength == nil && schema.MaxLength == nil {
		return "1"
	}
	return literal(doc, schema)
}

// literal renders a sample value as it appears in a URL: a query string and a
// path segment are text, not JSON, so a string is written bare rather than
// quoted.
func literal(doc *core.Document, schema *core.Schema) string {
	v := mock.Sample(doc, schema, nil)
	switch t := v.(type) {
	case nil:
		return "1"
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "1"
	}
	return string(data)
}

// nameOf labels a request. The operationId is what the API author chose, so it
// wins; the method and path are the honest fallback.
func nameOf(op *core.Operation, method, path string) string {
	if op.Summary != "" {
		return op.Summary
	}
	if op.OperationID != "" {
		return op.OperationID
	}
	return method + " " + path
}

// auth describes how the artefacts authenticate, in the one scheme they can
// carry. Header, name and variable are all that a request needs.
type auth struct {
	Header   string // "Authorization" or an API key header
	Prefix   string // "Bearer " for a bearer token, empty otherwise
	Variable string // the .http variable / env var stem: token, apiKey
	Env      string // environment variable the Go test and shell read
}

// authOf picks a scheme to carry. A document may list several as alternatives —
// any one satisfies a request — so the first by name is used, and which one it
// is stays visible in the generated file for a reader to change.
func authOf(doc *core.Document) *auth {
	schemes := doc.Components.SecuritySchemes
	if len(schemes) == 0 {
		return nil
	}
	names := make([]string, 0, len(schemes))
	for n := range schemes {
		names = append(names, n)
	}
	sort.Strings(names)

	s := schemes[names[0]]
	if s == nil {
		return nil
	}
	switch s.Type {
	case "apiKey":
		if s.In != "header" || s.Name == "" {
			// A key in a query or a cookie is carried differently and is not
			// worth guessing at; the request goes out unauthenticated and the
			// generated file says why.
			return nil
		}
		return &auth{Header: s.Name, Variable: "apiKey", Env: "SPECTER_API_KEY"}
	default:
		prefix := "Bearer "
		if s.Scheme == "basic" {
			prefix = "Basic "
		}
		return &auth{Header: "Authorization", Prefix: prefix, Variable: "token", Env: "SPECTER_TOKEN"}
	}
}

// render executes a template from the embedded set.
func render(name string, data any) ([]byte, error) {
	t, err := template.New(name).Funcs(funcs).ParseFS(assets, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gofmt formats generated Go source, and names the file when it does not
// parse: a template bug is otherwise reported as an error with no location.
func gofmt(name string, src []byte) ([]byte, error) {
	out, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

var funcs = template.FuncMap{
	"goName": goName,
	"quote":  strconv.Quote,
	"join":   strings.Join,
	"statusList": func(codes []int) string {
		parts := make([]string, len(codes))
		for i, c := range codes {
			parts[i] = strconv.Itoa(c)
		}
		return strings.Join(parts, ", ")
	},
}

// goName turns a request name into a legal, readable Go test suffix:
// "List users" -> "ListUsers", "GET /users/{id}" -> "GETUsersId".
func goName(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			if upper {
				b.WriteRune(r - 32)
				upper = false
			} else {
				b.WriteRune(r)
			}
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			upper = false
		default:
			// Anything else is a separator rather than a character.
			upper = true
		}
	}
	name := b.String()
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		return "Request" + name
	}
	return name
}
