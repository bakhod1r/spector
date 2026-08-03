package proxy

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/route"
)

// Learner builds a document fragment out of what the scanner missed.
//
// Static analysis cannot see everything: a route registered through a table, a
// handler behind an interface, an endpoint served by a library. Those endpoints
// exist, clients call them, and no amount of reading the source finds them —
// but they walk past the proxy every day.
//
// What comes out is a fragment, never a replacement. It is marked as inferred,
// because it is: derived from however many responses happened to go past, which
// is evidence about those responses and not a specification. Merging it is a
// decision for a person, so the output is shaped to be read and copied from
// rather than piped into the document.
type Learner struct {
	mu   sync.Mutex
	seen map[string]*observed
}

type observed struct {
	method   string
	path     string // normalised: /users/{id}
	statuses map[int]int
	// samples is the response body of the first success seen per status, which
	// is what a schema is inferred from.
	bodies map[int]any
	count  int
}

func NewLearner() *Learner {
	return &Learner{seen: map[string]*observed{}}
}

// Observe records an exchange. documentedPath is the template the request
// matched, or empty when it matched nothing — an endpoint the document has at
// all is only interesting for the statuses it turned out to answer.
func (l *Learner) Observe(ex Exchange, documentedPath string) {
	if l == nil {
		return
	}
	path := documentedPath
	if path == "" {
		path = NormalisePath(ex.Path)
	}
	key := ex.Method + " " + path

	l.mu.Lock()
	defer l.mu.Unlock()

	o := l.seen[key]
	if o == nil {
		o = &observed{method: ex.Method, path: path, statuses: map[int]int{}, bodies: map[int]any{}}
		l.seen[key] = o
	}
	o.count++
	o.statuses[ex.Status]++

	if _, have := o.bodies[ex.Status]; !have && len(ex.ResponseBody) > 0 {
		var value any
		if err := json.Unmarshal(ex.ResponseBody, &value); err == nil {
			o.bodies[ex.Status] = value
		}
	}
}

// Document renders what was observed as an OpenAPI-shaped fragment.
func (l *Learner) Document(title, version string) *core.Document {
	doc := core.NewDocument(title+" (observed)", version)

	l.mu.Lock()
	defer l.mu.Unlock()

	keys := make([]string, 0, len(l.seen))
	for k := range l.seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		o := l.seen[k]
		op := core.NewOperation("")
		op.Summary = "Observed in traffic, not found in the source"
		op.Description = "Inferred from " + strconv.Itoa(o.count) +
			" observed request(s). This is evidence about what happened, not a specification: review it before merging."

		for _, seg := range route.Split(o.path) {
			if len(seg) > 2 && seg[0] == '{' {
				op.Parameters = append(op.Parameters, core.Parameter{
					Name: strings.Trim(seg, "{}"), In: "path", Required: true,
					Schema: &core.Schema{Type: "string"},
				})
			}
		}
		// Parameters in map order would churn; the path order is meaningful.
		statuses := make([]int, 0, len(o.statuses))
		for code := range o.statuses {
			statuses = append(statuses, code)
		}
		sort.Ints(statuses)

		for _, code := range statuses {
			resp := &core.Response{
				Description: "Observed " + strconv.Itoa(o.statuses[code]) + " time(s)",
			}
			if body, ok := o.bodies[code]; ok {
				if schema := InferSchema(body); schema != nil {
					resp.Content = map[string]core.MediaType{"application/json": {Schema: schema}}
				}
			}
			op.Responses[strconv.Itoa(code)] = resp
		}
		doc.AddOperation(o.path, strings.ToLower(o.method), op)
	}
	return doc
}

// Empty reports whether anything was learned, so a caller does not write a file
// saying nothing.
func (l *Learner) Empty() bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.seen) == 0
}

// idLike recognises a path segment that is an identifier rather than a name.
// Without this every id becomes its own endpoint and the fragment is a list of
// thousands of paths instead of one.
var (
	numeric = regexp.MustCompile(`^\d+$`)
	uuid    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	hexID   = regexp.MustCompile(`^[0-9a-fA-F]{16,}$`)
)

// NormalisePath turns a request path into a template by replacing segments that
// look like identifiers.
//
// It is a guess, and a conservative one: only unmistakable identifier shapes
// are collapsed. A slug like /posts/hello-world stays literal, because
// collapsing it would claim an endpoint the API may not have.
func NormalisePath(path string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		switch {
		case numeric.MatchString(s):
			segs[i] = "{id}"
		case uuid.MatchString(s), hexID.MatchString(s):
			segs[i] = "{id}"
		}
	}
	return strings.Join(segs, "/")
}

// InferSchema derives a schema from one decoded JSON value.
//
// Everything present is documented as required, which is right for the evidence
// available: the field was there. Over more samples that would be too strong a
// claim, and the description says how many samples there were so a reader can
// weigh it.
func InferSchema(value any) *core.Schema {
	switch v := value.(type) {
	case nil:
		return nil
	case bool:
		return &core.Schema{Type: "boolean"}
	case string:
		return &core.Schema{Type: "string"}
	case float64:
		if v == float64(int64(v)) {
			return &core.Schema{Type: "integer"}
		}
		return &core.Schema{Type: "number"}
	case []any:
		s := &core.Schema{Type: "array"}
		if len(v) > 0 {
			s.Items = InferSchema(v[0])
		}
		return s
	case map[string]any:
		s := &core.Schema{Type: "object", Properties: map[string]*core.Schema{}}
		names := make([]string, 0, len(v))
		for name := range v {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if prop := InferSchema(v[name]); prop != nil {
				s.Properties[name] = prop
				s.Required = append(s.Required, name)
			}
		}
		return s
	}
	return nil
}
