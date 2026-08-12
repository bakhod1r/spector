// Package export renders a generated document in formats other tools consume
// directly: a Postman collection and Markdown. Insomnia imports Postman
// collections natively, so one collection format serves both clients.
package export

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/mock"
)

// postman collection v2.1 — the minimal subset every importer understands.
type pmCollection struct {
	Info     pmInfo       `json:"info"`
	Item     []pmItem     `json:"item"`
	Auth     *pmAuth      `json:"auth,omitempty"`
	Variable []pmVariable `json:"variable,omitempty"`
}

type pmVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

type pmInfo struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
}

type pmItem struct {
	Name     string       `json:"name"`
	Item     []pmItem     `json:"item,omitempty"`    // folder
	Request  *pmRequest   `json:"request,omitempty"` // leaf
	Response []pmResponse `json:"response,omitempty"`
	Event    []pmEvent    `json:"event,omitempty"`
}

// pmResponse is a saved example response Postman shows alongside the request.
type pmResponse struct {
	Name            string     `json:"name"`
	Status          string     `json:"status"` // reason phrase, e.g. "OK"
	Code            int        `json:"code"`
	Header          []pmHeader `json:"header,omitempty"`
	Body            string     `json:"body"`
	PreviewLanguage string     `json:"_postman_previewlanguage,omitempty"`
}

type pmEvent struct {
	Listen string   `json:"listen"` // "test" | "prerequest"
	Script pmScript `json:"script"`
}

type pmScript struct {
	Type string   `json:"type"` // "text/javascript"
	Exec []string `json:"exec"` // one line per element, the format Postman stores
}

type pmRequest struct {
	Method      string     `json:"method"`
	Header      []pmHeader `json:"header,omitempty"`
	Body        *pmBody    `json:"body,omitempty"`
	URL         pmURL      `json:"url"`
	Description string     `json:"description,omitempty"`
}

type pmHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type pmBody struct {
	Mode    string         `json:"mode"`
	Raw     string         `json:"raw"`
	Options map[string]any `json:"options,omitempty"`
}

type pmURL struct {
	Raw   string    `json:"raw"`
	Host  []string  `json:"host"`
	Path  []string  `json:"path"`
	Query []pmQuery `json:"query,omitempty"`
}

type pmQuery struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled,omitempty"`
}

type pmAuth struct {
	Type   string           `json:"type"`
	Bearer []map[string]any `json:"bearer,omitempty"`
	APIKey []map[string]any `json:"apikey,omitempty"`
	Basic  []map[string]any `json:"basic,omitempty"`
}

// Postman renders the document as a Postman collection v2.1. Every operation
// becomes one request, grouped into folders by first tag (or first path
// segment when there are no tags), with an example body sampled from the
// request schema so the request is runnable as imported.
//
// The base URL is a {{baseUrl}} variable rather than a literal: a collection
// is used against dev, staging and prod, and Postman environments exist for
// exactly this. The document's first server, if any, seeds the raw URL so an
// importer who sets nothing still has a working default.
func Postman(doc *core.Document) ([]byte, error) {
	base := "{{baseUrl}}"
	col := pmCollection{
		Info: pmInfo{
			Name:   doc.Info.Title,
			Schema: "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		Auth:     collectionAuth(doc),
		Variable: collectionVariables(doc),
	}

	// Folders keyed by group name, filled in path order for reproducibility.
	folders := map[string][]pmItem{}
	var order []string

	for _, path := range sortedPaths(doc) {
		for _, method := range sortedMethods(doc.Paths[path]) {
			op := doc.Paths[path][method]
			group := groupOf(op, path)
			if _, seen := folders[group]; !seen {
				order = append(order, group)
			}
			folders[group] = append(folders[group], operationItem(doc, base, path, method, op))
		}
	}

	for _, name := range order {
		col.Item = append(col.Item, pmItem{Name: name, Item: folders[name]})
	}
	return json.MarshalIndent(col, "", "  ")
}

// collectionVariables seeds the variables a collection references so it imports
// with working defaults and no environment attached. baseUrl always exists —
// empty when the document names no server, so the importer sees the field to
// fill rather than a missing one — followed by a placeholder for the auth
// scheme the collection declares, matching collectionAuth's choice.
func collectionVariables(doc *core.Document) []pmVariable {
	base := ""
	if len(doc.Servers) > 0 {
		base = doc.Servers[0].URL
	}
	vars := []pmVariable{{Key: "baseUrl", Value: base, Type: "string"}}
	switch auth := collectionAuth(doc); {
	case auth == nil:
	case auth.Type == "bearer":
		vars = append(vars, pmVariable{Key: "bearerToken", Value: "", Type: "string"})
	case auth.Type == "basic":
		vars = append(vars,
			pmVariable{Key: "basicUsername", Value: "", Type: "string"},
			pmVariable{Key: "basicPassword", Value: "", Type: "string"})
	case auth.Type == "apikey":
		vars = append(vars, pmVariable{Key: "apiKey", Value: "", Type: "string"})
	}
	return vars
}

// Postman environment format: a named bag of key/value pairs an importer picks
// from a dropdown, kept separate from the collection so one collection runs
// against dev, staging and prod.
type pmEnvironment struct {
	Name   string       `json:"name"`
	Values []pmEnvValue `json:"values"`
	Scope  string       `json:"_postman_variable_scope"`
}

type pmEnvValue struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
	Type    string `json:"type,omitempty"` // "secret" masks the value in Postman
}

// PostmanEnvironment renders a fillable Postman environment carrying the same
// variables the collection references: baseUrl seeded from the first server and
// a placeholder per auth credential. Credentials are typed "secret" so Postman
// masks them. It pairs with Postman: import the collection once, then switch
// environments to point it at a different deployment.
func PostmanEnvironment(doc *core.Document) ([]byte, error) {
	name := doc.Info.Title
	if name == "" {
		name = "API"
	}
	env := pmEnvironment{Name: name + " Environment", Scope: "environment"}
	for _, v := range collectionVariables(doc) {
		typ := ""
		if v.Key != "baseUrl" {
			typ = "secret"
		}
		env.Values = append(env.Values, pmEnvValue{
			Key: v.Key, Value: v.Value, Enabled: true, Type: typ,
		})
	}
	return json.MarshalIndent(env, "", "  ")
}

func collectionAuth(doc *core.Document) *pmAuth {
	// One scheme is representable as collection-level auth; pick the first
	// declared alternative, sorted so the choice is stable.
	names := make([]string, 0, len(doc.Components.SecuritySchemes))
	for name := range doc.Components.SecuritySchemes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := doc.Components.SecuritySchemes[name]
		switch {
		case s.Type == "http" && s.Scheme == "bearer":
			return &pmAuth{Type: "bearer", Bearer: []map[string]any{
				{"key": "token", "value": "{{bearerToken}}", "type": "string"},
			}}
		case s.Type == "http" && s.Scheme == "basic":
			return &pmAuth{Type: "basic", Basic: []map[string]any{
				{"key": "username", "value": "{{basicUsername}}", "type": "string"},
				{"key": "password", "value": "{{basicPassword}}", "type": "string"},
			}}
		case s.Type == "apiKey":
			in := s.In
			if in == "" {
				in = "header"
			}
			return &pmAuth{Type: "apikey", APIKey: []map[string]any{
				{"key": "key", "value": s.Name, "type": "string"},
				{"key": "value", "value": "{{apiKey}}", "type": "string"},
				{"key": "in", "value": in, "type": "string"},
			}}
		}
	}
	return nil
}

func operationItem(doc *core.Document, base, path, method string, op *core.Operation) pmItem {
	name := op.Summary
	if name == "" {
		name = strings.ToUpper(method) + " " + path
	}

	// {id} becomes :id, which Postman renders as an editable path variable.
	segs := []string{}
	for _, s := range strings.Split(strings.Trim(path, "/"), "/") {
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			s = ":" + strings.Trim(s, "{}")
		}
		segs = append(segs, s)
	}

	req := &pmRequest{
		Method:      strings.ToUpper(method),
		Description: op.Description,
		URL: pmURL{
			Raw:  base + "/" + strings.Join(segs, "/"),
			Host: []string{base},
			Path: segs,
		},
	}

	for _, p := range op.Parameters {
		param := resolveParam(doc, p)
		switch param.In {
		case "query":
			req.URL.Query = append(req.URL.Query, pmQuery{
				Key: param.Name, Value: "", Disabled: !param.Required,
			})
		case "header":
			req.Header = append(req.Header, pmHeader{Key: param.Name, Value: ""})
		}
	}

	if op.RequestBody != nil {
		if media, ok := op.RequestBody.Content["application/json"]; ok && media.Schema != nil {
			body, err := json.MarshalIndent(mock.Sample(doc, media.Schema, nil), "", "  ")
			if err == nil {
				req.Header = append(req.Header, pmHeader{Key: "Content-Type", Value: "application/json"})
				req.Body = &pmBody{
					Mode: "raw",
					Raw:  string(body),
					Options: map[string]any{
						"raw": map[string]string{"language": "json"},
					},
				}
			}
		}
	}

	return pmItem{Name: name, Request: req, Event: testEvent(op), Response: responseExamples(doc, op)}
}

// responseExamples turns each documented response carrying a JSON body into a
// saved example with a sampled body, in status order. A response with no JSON
// body (an error status, a 204) contributes nothing — there is no shape to show.
func responseExamples(doc *core.Document, op *core.Operation) []pmResponse {
	codes := make([]string, 0, len(op.Responses))
	for code := range op.Responses {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	var out []pmResponse
	for _, code := range codes {
		resp := op.Responses[code]
		if resp == nil {
			continue
		}
		media, ok := resp.Content["application/json"]
		if !ok || media.Schema == nil {
			continue
		}
		body, err := json.MarshalIndent(mock.Sample(doc, media.Schema, nil), "", "  ")
		if err != nil {
			continue
		}
		n, _ := strconv.Atoi(code)
		out = append(out, pmResponse{
			Name:            resp.Description,
			Status:          http.StatusText(n),
			Code:            n,
			Header:          []pmHeader{{Key: "Content-Type", Value: "application/json"}},
			Body:            string(body),
			PreviewLanguage: "json",
		})
	}
	return out
}

// testEvent builds a Postman test script that asserts the response status is
// one the operation documents, and — when a success response documents a JSON
// body — that the body parses. An operation with no documented responses gets
// no script: there is nothing to assert against.
func testEvent(op *core.Operation) []pmEvent {
	codes := make([]string, 0, len(op.Responses))
	jsonBody := false
	for code, resp := range op.Responses {
		codes = append(codes, code)
		if strings.HasPrefix(code, "2") && resp != nil {
			if media, ok := resp.Content["application/json"]; ok && media.Schema != nil {
				jsonBody = true
			}
		}
	}
	if len(codes) == 0 {
		return nil
	}
	sort.Strings(codes)

	list, _ := json.Marshal(codes) // e.g. ["200","404"] — always marshals
	exec := []string{
		"pm.test(\"status is documented\", function () {",
		"    pm.expect(" + string(list) + ").to.include(String(pm.response.code));",
		"});",
	}
	if jsonBody {
		exec = append(exec,
			"pm.test(\"response is valid JSON\", function () {",
			"    pm.response.json();",
			"});",
		)
	}
	return []pmEvent{{Listen: "test", Script: pmScript{Type: "text/javascript", Exec: exec}}}
}

// resolveParam follows a $ref into components/parameters; a dangling ref comes
// back empty, which renders as nothing rather than a broken entry.
func resolveParam(doc *core.Document, p core.Parameter) core.Parameter {
	if p.Ref == "" {
		return p
	}
	name := strings.TrimPrefix(p.Ref, "#/components/parameters/")
	if resolved, ok := doc.Components.Parameters[name]; ok && resolved != nil {
		return *resolved
	}
	return core.Parameter{}
}

func groupOf(op *core.Operation, path string) string {
	if len(op.Tags) > 0 {
		return op.Tags[0]
	}
	for _, s := range strings.Split(strings.Trim(path, "/"), "/") {
		if s != "" && !strings.HasPrefix(s, "{") {
			return s
		}
	}
	return "root"
}

func sortedPaths(doc *core.Document) []string {
	out := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// methodOrder puts methods in the order a reader expects rather than
// alphabetical, which would file DELETE before GET.
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
