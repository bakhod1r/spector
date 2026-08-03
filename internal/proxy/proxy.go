// Package proxy watches live traffic and compares it against the document.
//
// The contract artefacts check the document with requests Specter invented:
// one sample body, one path value, the happy path. Real traffic is not like
// that. It has empty lists, error cases, clients sending fields nobody
// documented, and endpoints the scanner never saw because they are registered
// somewhere it does not read. Those are exactly the places a document goes
// stale, and exactly the places a generated test will not look.
//
// So this sits in front of the real API, forwards everything, and reports where
// what went past disagrees with what was written down.
//
// One rule governs the design: the proxy never breaks a request. Analysis
// happens after the response has been sent, a panic in it is contained, and no
// finding is worth degrading the service being observed.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/user/specter/internal/conform"
	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/route"
)

// Kinds of disagreement worth reporting.
const (
	// KindUndocumentedEndpoint is traffic to an operation the document does not
	// have at all — the scanner missed it, or it was added and never written
	// down.
	KindUndocumentedEndpoint = "undocumented-endpoint"
	// KindUndocumentedStatus is a documented endpoint answering with a code its
	// document does not list. Every generated client handles the documented
	// ones; this is the one it will not.
	KindUndocumentedStatus = "undocumented-status"
	// KindContentType is a body arriving as something other than the media type
	// promised.
	KindContentType = "content-type"
	// KindShape is a response body contradicting its schema.
	KindShape = "shape"
	// KindUndocumentedField is a property in a response the document does not
	// mention. Reported, never fatal: it means the API grew.
	KindUndocumentedField = "undocumented-field"
)

// Finding is one disagreement, with how often it was seen. Aggregation is the
// point: a drift on a busy endpoint happens hundreds of times a minute, and a
// report with hundreds of identical lines is one nobody reads.
type Finding struct {
	Kind   string `json:"kind"`
	Method string `json:"method"`
	// Path is the documented template where one matched, and the literal
	// request path where none did.
	Path   string `json:"path"`
	Detail string `json:"detail,omitempty"`
	Count  int    `json:"count"`
	// First is the request path that produced this finding first, kept because
	// a template does not tell you what to curl to see it again.
	First string `json:"firstSeen,omitempty"`
}

func (f Finding) String() string {
	s := fmt.Sprintf("%s: %s %s", f.Kind, f.Method, f.Path)
	if f.Detail != "" {
		s += ": " + f.Detail
	}
	return s
}

// Options configures the proxy.
type Options struct {
	// Target is the API being watched, e.g. http://localhost:3000.
	Target string
	// OnFinding is called once per *new* finding, for a live log. Repeats only
	// increment the count.
	OnFinding func(Finding)
	// Recorder, when set, is handed every exchange.
	Recorder *Recorder
	// Learner, when set, accumulates what the document does not have.
	Learner *Learner
}

// Proxy is a running comparison between a document and the traffic reaching an
// API.
type Proxy struct {
	handler http.Handler

	mu       sync.Mutex
	findings map[string]*Finding
	requests int
}

// New builds a proxy for doc that forwards to opts.Target.
func New(doc *core.Document, opts Options) (*Proxy, error) {
	if opts.Target == "" {
		return nil, fmt.Errorf("no target: the proxy forwards to a real API, so it needs one (e.g. -proxy-target http://localhost:3000)")
	}
	target, err := url.Parse(opts.Target)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", opts.Target, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("target %q is not an absolute URL (try http://localhost:3000)", opts.Target)
	}

	p := &Proxy{findings: map[string]*Finding{}}
	routes := route.Compile(doc)
	components := componentsOf(doc)

	rp := httputil.NewSingleHostReverseProxy(target)
	// The default error handler answers 502 with no explanation, which reads as
	// the API being down rather than the proxy failing to reach it.
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title":  "the proxy could not reach the target",
			"detail": err.Error(),
			"target": opts.Target,
		})
	}

	p.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody := drain(r)

		rec := &capture{ResponseWriter: w, status: http.StatusOK}
		rp.ServeHTTP(rec, r)

		// Everything below is observation. It runs after the client has its
		// response, and a bug in it must not become a bug in the API being
		// watched.
		defer func() { _ = recover() }()

		p.mu.Lock()
		p.requests++
		p.mu.Unlock()

		exchange := Exchange{
			Method:       r.Method,
			Path:         r.URL.Path,
			Query:        r.URL.RawQuery,
			Status:       rec.status,
			RequestBody:  reqBody,
			ResponseBody: rec.body.Bytes(),
			ReqHeader:    r.Header.Clone(),
			ResHeader:    rec.Header().Clone(),
		}
		if opts.Recorder != nil {
			opts.Recorder.Record(exchange)
		}
		p.inspect(routes, components, exchange, opts)
	})

	return p, nil
}

// Handler is the http.Handler to serve.
func (p *Proxy) Handler() http.Handler { return p.handler }

// inspect compares one exchange against the document.
func (p *Proxy) inspect(routes []route.Route, components map[string]*conform.Schema, ex Exchange, opts Options) {
	rt, _, ok := route.Match(routes, ex.Method, ex.Path)
	if !ok {
		p.report(opts, Finding{
			Kind: KindUndocumentedEndpoint, Method: ex.Method, Path: ex.Path,
			Detail: "no documented operation matches this request",
		}, ex.Path)
		if opts.Learner != nil {
			opts.Learner.Observe(ex, "")
		}
		return
	}

	status := strconv.Itoa(ex.Status)
	resp, documented := rt.Op.Responses[status]
	if !documented {
		p.report(opts, Finding{
			Kind: KindUndocumentedStatus, Method: ex.Method, Path: rt.Path,
			Detail: fmt.Sprintf("answered %d; documented: %s", ex.Status, documentedCodes(rt.Op)),
		}, ex.Path)
		if opts.Learner != nil {
			opts.Learner.Observe(ex, rt.Path)
		}
		return
	}

	// Nothing was promised about this response's body, so there is nothing it
	// can contradict.
	media, hasJSON := resp.Content["application/json"]
	if !hasJSON || media.Schema == nil {
		return
	}

	if ct := ex.ResHeader.Get("Content-Type"); !strings.Contains(ct, "json") {
		p.report(opts, Finding{
			Kind: KindContentType, Method: ex.Method, Path: rt.Path,
			Detail: fmt.Sprintf("answered %q where the document promises JSON", ct),
		}, ex.Path)
		return
	}

	var value any
	if err := json.Unmarshal(ex.ResponseBody, &value); err != nil {
		p.report(opts, Finding{
			Kind: KindShape, Method: ex.Method, Path: rt.Path,
			Detail: "the response is not valid JSON: " + err.Error(),
		}, ex.Path)
		return
	}

	schema := toConform(media.Schema)
	for _, problem := range conform.Check(components, schema, value, "response") {
		p.report(opts, Finding{
			Kind: KindShape, Method: ex.Method, Path: rt.Path, Detail: problem,
		}, ex.Path)
	}
	for _, extra := range conform.Undocumented(components, schema, value, "response") {
		p.report(opts, Finding{
			Kind: KindUndocumentedField, Method: ex.Method, Path: rt.Path, Detail: extra,
		}, ex.Path)
	}
}

// report records a finding, merging it with an identical earlier one.
func (p *Proxy) report(opts Options, f Finding, requestPath string) {
	key := f.Kind + "\x00" + f.Method + "\x00" + f.Path + "\x00" + f.Detail

	p.mu.Lock()
	existing, seen := p.findings[key]
	if seen {
		existing.Count++
		p.mu.Unlock()
		return
	}
	f.Count = 1
	f.First = requestPath
	p.findings[key] = &f
	p.mu.Unlock()

	// Only the first sighting is announced. A drift on a busy endpoint would
	// otherwise fill the terminal with the same line.
	if opts.OnFinding != nil {
		opts.OnFinding(f)
	}
}

// Findings returns what has been seen, most frequent first. The order is total,
// so two runs over the same traffic produce the same report and a diff between
// them means something.
func (p *Proxy) Findings() []Finding {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]Finding, 0, len(p.findings))
	for _, f := range p.findings {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].Detail < out[j].Detail
	})
	return out
}

// Requests is how much traffic has been seen, which is what makes a report of
// no findings meaningful: zero findings over zero requests says nothing.
func (p *Proxy) Requests() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests
}

// Report is the JSON artefact, shaped so a CI job can diff two of them.
type Report struct {
	Target   string    `json:"target"`
	Requests int       `json:"requests"`
	Findings []Finding `json:"findings"`
}

func (p *Proxy) Report(target string) Report {
	return Report{Target: target, Requests: p.Requests(), Findings: p.Findings()}
}

// capture is a ResponseWriter that keeps what was written, so the body can be
// examined after the client already has it.
type capture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
	// wroteHeader guards against a handler calling WriteHeader twice, which is
	// legal for the proxy to observe and illegal to pass on.
	wroteHeader bool
}

func (c *capture) WriteHeader(status int) {
	if c.wroteHeader {
		return
	}
	c.wroteHeader = true
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

func (c *capture) Write(b []byte) (int, error) {
	// A body without an explicit WriteHeader is a 200, per net/http.
	if !c.wroteHeader {
		c.wroteHeader = true
	}
	// Bounded: a streaming endpoint would otherwise be buffered in full, and
	// the checker only needs enough to parse.
	if c.body.Len() < maxBody {
		c.body.Write(b)
	}
	return c.ResponseWriter.Write(b)
}

// Flush keeps streaming endpoints — SSE, long polls — working through the
// proxy. Without it the client waits for a response that is being held in a
// buffer it cannot see.
func (c *capture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// maxBody bounds what is kept from a response. A megabyte is far more than any
// schema check needs and far less than a file download.
const maxBody = 1 << 20

// drain reads a request body and puts it back, so the handler downstream still
// receives it.
func drain(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return data
}

func documentedCodes(op *core.Operation) string {
	codes := make([]string, 0, len(op.Responses))
	for code := range op.Responses {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	if len(codes) == 0 {
		return "none"
	}
	return strings.Join(codes, ", ")
}

// toConform converts a document schema into the checker's own shape. The
// conversion is a JSON round-trip on purpose: it is exactly what the generated
// contract tests do at run time, so both paths reach the checker the same way.
func toConform(s *core.Schema) *conform.Schema {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	var out conform.Schema
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return &out
}

func componentsOf(doc *core.Document) map[string]*conform.Schema {
	out := map[string]*conform.Schema{}
	if doc == nil {
		return out
	}
	for name, s := range doc.Components.Schemas {
		if c := toConform(s); c != nil {
			out[name] = c
		}
	}
	return out
}
