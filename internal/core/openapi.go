// Package core defines the OpenAPI 3.0 document model and shared types
// that all framework adapters produce.
package core

import (
	"go/token"
	"strconv"
)

// Diagnostic records a route site an adapter could not statically resolve to
// a literal string. It lives in core (rather than astutil) so the Adapter
// interface can reference it without astutil importing core creating an
// import cycle; astutil.Diagnostic is a type alias for this.
type Diagnostic struct {
	Pos    token.Position
	Kind   string // "route" | "group-prefix"
	Reason string
}

// Document is a minimal OpenAPI 3.0 document.
type Document struct {
	OpenAPI string                           `json:"openapi"`
	Info    Info                             `json:"info"`
	Servers []Server                         `json:"servers,omitempty"`
	Paths   map[string]map[string]*Operation `json:"paths"`
	// Security lists the schemes that satisfy a request. Entries are
	// alternatives: any one of them is enough.
	Security   []SecurityRequirement `json:"security,omitempty"`
	Components Components            `json:"components"`
	// Diagnostics lists the route/group-prefix sites the adapter could not
	// statically resolve to a literal string. It is not part of the OpenAPI
	// spec and is excluded from the document's JSON/YAML encoding.
	Diagnostics []Diagnostic `json:"-"`
}

// Server is one base URL the API is reachable at. The yaml tags let a YAML
// config file (spector.yaml) use the same keys as JSON; they never affect the
// document's JSON output.
type Server struct {
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// SecurityRequirement names a scheme from components.securitySchemes. The
// value holds required scopes, which only apply to OAuth2/OpenID flows and is
// otherwise an empty list.
type SecurityRequirement map[string][]string

// SecurityScheme describes how a caller authenticates. Only the http and
// apiKey types are modelled — they cover bearer tokens, basic auth, and API
// keys, which is what the console can actually send.
type SecurityScheme struct {
	Type         string `json:"type" yaml:"type"`                                     // "http" | "apiKey"
	Scheme       string `json:"scheme,omitempty" yaml:"scheme,omitempty"`             // http: "bearer" | "basic"
	BearerFormat string `json:"bearerFormat,omitempty" yaml:"bearerFormat,omitempty"` // http+bearer: e.g. "JWT"
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`                 // apiKey: parameter name
	In           string `json:"in,omitempty" yaml:"in,omitempty"`                     // apiKey: "header" | "query" | "cookie"
	Description  string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Info holds API metadata.
type Info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

// Operation is a single method on a path (e.g. GET /users).
type Operation struct {
	Summary     string               `json:"summary,omitempty"`
	Description string               `json:"description,omitempty"`
	OperationID string               `json:"operationId,omitempty"`
	Tags        []string             `json:"tags,omitempty"`
	Deprecated  bool                 `json:"deprecated,omitempty"`
	Parameters  []Parameter          `json:"parameters,omitempty"`
	RequestBody *RequestBody         `json:"requestBody,omitempty"`
	Responses   map[string]*Response `json:"responses"`

	// Source is where the handler is defined. "x-" marks it as a vendor
	// extension, which the OpenAPI spec permits anywhere and every validator
	// and consumer ignores, so carrying it costs compatibility nothing.
	Source *Source `json:"x-spector-source,omitempty"`

	// Calls is what the handler reaches outside the process. Also a vendor
	// extension, and also inferred, so each entry carries its confidence.
	Calls []Call `json:"x-spector-calls,omitempty"`

	// Realtime marks an operation that upgrades or streams rather than
	// returning a body. Without it the document describes these endpoints
	// wrongly, as ordinary GETs that return nothing.
	Realtime string `json:"x-spector-realtime,omitempty"`

	// Middleware is what runs in front of the handler. Authentication is the
	// reason this is here: it never appears in the handler body, so without it
	// a protected endpoint documents as public.
	Middleware []Middleware `json:"x-spector-middleware,omitempty"`

	// Security lists the schemes a caller must satisfy, inferred from
	// authentication middleware. Entries here are requirements, not
	// alternatives: two auth middlewares mean both run.
	Security []SecurityRequirement `json:"security,omitempty"`

	// Advice is where this operation diverges from an HTTP or JSON standard.
	// Recommendations only: Spector documents what the code does and never
	// reshapes it to look better than it is.
	Advice []Advisory `json:"x-spector-advice,omitempty"`

	// Observed marks an operation that came from observed traffic, folded in by
	// MergeObserved, rather than from source. "x-" makes it a vendor extension
	// consumers ignore; omitempty keeps every source-only document unchanged.
	Observed bool `json:"x-spector-observed,omitempty"`

	// Manual marks an operation declared by hand in the config file's routes:
	// list rather than found in source. It is how a genuinely dynamic route —
	// one the AST cannot resolve, which otherwise only emits a diagnostic —
	// gets into the document, and it stays labelled so a reader can tell an
	// asserted route from a scanned one.
	Manual bool `json:"x-spector-manual,omitempty"`
}

// OperationOption configures an Operation built with NewOperation.
type OperationOption func(*Operation)

// WithSummary sets the operation's short summary.
func WithSummary(s string) OperationOption {
	return func(o *Operation) { o.Summary = s }
}

// WithDescription sets the operation's long description.
func WithDescription(d string) OperationOption {
	return func(o *Operation) { o.Description = d }
}

// WithParameter appends a path/query/header parameter.
func WithParameter(p Parameter) OperationOption {
	return func(o *Operation) { o.Parameters = append(o.Parameters, p) }
}

// WithSource records where the handler is defined.
func WithSource(s *Source) OperationOption {
	return func(o *Operation) { o.Source = s }
}

// WithCalls records what the handler reaches outside the process.
func WithCalls(cs []Call) OperationOption {
	return func(o *Operation) { o.Calls = cs }
}

// WithTags groups the operation. Tags cannot be inferred from routing code —
// a path prefix is a guess, not a grouping — so they come from a directive.
func WithTags(tags []string) OperationOption {
	return func(o *Operation) { o.Tags = tags }
}

// WithDeprecated marks the operation as deprecated.
func WithDeprecated(v bool) OperationOption {
	return func(o *Operation) { o.Deprecated = v }
}

// WithMiddleware records what runs in front of the handler.
func WithMiddleware(ms []Middleware) OperationOption {
	return func(o *Operation) { o.Middleware = ms }
}

// WithSecurity sets the operation's security requirements.
func WithSecurity(reqs []SecurityRequirement) OperationOption {
	return func(o *Operation) { o.Security = reqs }
}

// WithRealtime marks the operation as a WebSocket or SSE endpoint.
func WithRealtime(kind string) OperationOption {
	return func(o *Operation) { o.Realtime = kind }
}

// WithRequestBody sets the request body.
func WithRequestBody(rb *RequestBody) OperationOption {
	return func(o *Operation) { o.RequestBody = rb }
}

// NewOperation builds an Operation with an initialized response map, applying
// the given options in order.
func NewOperation(id string, opts ...OperationOption) *Operation {
	op := &Operation{OperationID: id, Responses: map[string]*Response{}}
	for _, opt := range opts {
		opt(op)
	}
	return op
}

// SetResponse registers a response under an HTTP status code. Status 0 means
// the scanner saw a response but could not resolve its code — the handler
// wrote it through a variable — and OpenAPI has a key for exactly that:
// "default". Filing it under 200 instead would put a number in the document
// that the source never contained.
func (o *Operation) SetResponse(status int, r *Response) {
	if status <= 0 {
		o.Responses["default"] = r
		return
	}
	o.Responses[strconv.Itoa(status)] = r
}

// Parameter is a path/query parameter.
type Parameter struct {
	// Ref points at components/parameters. When it is set the rest is empty:
	// OpenAPI treats a $ref entry as a replacement, not a partial override.
	Ref string `json:"$ref,omitempty"`

	Name string `json:"name,omitempty"`
	In   string `json:"in,omitempty"` // "path" | "query" | "header"
	// omitempty is safe here: false is the default in OpenAPI, so an omitted
	// required reads as optional, which is what it means.
	Required bool    `json:"required,omitempty"`
	Schema   *Schema `json:"schema,omitempty"`

	Description string `json:"description,omitempty"`
}

// RequestBody describes an incoming payload.
type RequestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]MediaType `json:"content"`
}

// Response describes an outgoing payload.
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

// MediaType wraps a schema for a content type.
type MediaType struct {
	Schema *Schema `json:"schema"`
}

// ResponseOption configures a Response built with NewResponse.
type ResponseOption func(*Response)

// WithJSONBody attaches an application/json body schema to the response.
func WithJSONBody(schema *Schema) ResponseOption {
	return func(r *Response) {
		if schema == nil {
			return
		}
		if r.Content == nil {
			r.Content = map[string]MediaType{}
		}
		r.Content["application/json"] = MediaType{Schema: schema}
	}
}

// NewResponse builds a Response with the given description and options.
func NewResponse(description string, opts ...ResponseOption) *Response {
	r := &Response{Description: description}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Schema is a JSON Schema fragment. Ref points to components/schemas.
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	Enum                 []any              `json:"enum,omitempty"`

	// XOneof records groups of mutually-exclusive property names generated
	// from proto `oneof` fields. Each inner slice is one oneof group's
	// variant names.
	XOneof [][]string `json:"x-oneof,omitempty"`

	// Description and Example are pure documentation, read from `doc:` and
	// `example:` tags. They constrain nothing, which is exactly why they are
	// worth reading: there is no way for them to make a document wrong.
	Description string `json:"description,omitempty"`
	Example     any    `json:"example,omitempty"`

	// Default is what the server uses when the caller omits the value, read
	// from c.DefaultQuery("limit", "20"). It is documentation of an existing
	// behaviour, not a constraint: the endpoint already behaves this way and
	// the client had no way to find out.
	Default any       `json:"default,omitempty"`
	AllOf   []*Schema `json:"allOf,omitempty"`

	// Constraints read from validation tags. Every one is omitempty, so a
	// document generated from code without tags is byte-identical to before.
	//
	// The numeric and length bounds are pointers because zero is a meaningful
	// value: `min=0` states that negatives are rejected, which is not the same
	// as saying nothing about the minimum. A plain int cannot tell those apart.
	Required         []string `json:"required,omitempty"`
	Minimum          *float64 `json:"minimum,omitempty"`
	Maximum          *float64 `json:"maximum,omitempty"`
	ExclusiveMinimum bool     `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum bool     `json:"exclusiveMaximum,omitempty"`
	MinLength        *int     `json:"minLength,omitempty"`
	MaxLength        *int     `json:"maxLength,omitempty"`
	MinItems         *int     `json:"minItems,omitempty"`
	MaxItems         *int     `json:"maxItems,omitempty"`
}

// Components holds reusable schemas and security schemes.
type Components struct {
	Schemas map[string]*Schema `json:"schemas"`
	// Parameters holds parameters shared by many operations — in practice the
	// headers a middleware requires, which would otherwise be repeated
	// identically on every route it guards.
	Parameters      map[string]*Parameter      `json:"parameters,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes,omitempty"`
}

// NewDocument returns an empty, initialized OpenAPI 3.0 document.
func NewDocument(title, version string) *Document {
	return &Document{
		OpenAPI: "3.0.3",
		Info:    Info{Title: title, Version: version},
		Paths:   map[string]map[string]*Operation{},
		Components: Components{
			Schemas: map[string]*Schema{},
		},
	}
}

// AddOperation registers an operation under a path + method.
func (d *Document) AddOperation(path, method string, op *Operation) {
	if d.Paths[path] == nil {
		d.Paths[path] = map[string]*Operation{}
	}
	d.Paths[path][method] = op
}

// Route is the framework-agnostic result an adapter emits per endpoint.
type Route struct {
	Method        string // GET, POST, ...
	Path          string // /users/:id  (adapter normalizes to /users/{id})
	HandlerName   string
	RequestType   string // Go type name bound from body, "" if none
	RequestArray  bool   // body is []RequestType
	ResponseType  string // Go type name returned, "" if none
	ResponseArray bool   // response is []ResponseType
	QueryParams   []string
	// QueryDefaults holds the fallback value of the query parameters that have
	// one, keyed by name; parameters without a default are absent.
	QueryDefaults map[string]string
	HeaderParams  []string
	Summary       string          // first line of the handler doc comment
	Description   string          // remaining lines of the handler doc comment
	Responses     []RouteResponse // status-coded responses; falls back to ResponseType when empty
	Source        *Source         // where the handler is defined; nil when unknown
	Calls         []Call          // what the handler reaches outside the process
	Realtime      string          // "websocket" | "sse" | "" for an ordinary handler
	Tags          []string        // from a spector:tags directive; grouping the AST cannot infer
	Deprecated    bool            // from a spector:deprecated directive
	OperationID   string          // from a spector:operationId directive; overrides the handler name
	Middleware    []Middleware    // what runs in front of the handler
}

// Call is something a handler reaches outside the process: a database, another
// service, a cache, a queue.
type Call struct {
	Kind       string `json:"kind"`   // CallDB, CallHTTP, CallGRPC, CallCache, CallQueue
	Target     string `json:"target"` // what was called, e.g. "db.QueryContext"
	Confidence string `json:"confidence"`
}

// The kinds of dependency a handler can have.
const (
	CallDB    = "db"
	CallHTTP  = "http"
	CallGRPC  = "grpc"
	CallCache = "cache"
	CallQueue = "queue"
)

// Confidence records how a call was identified, because the two ways are not
// equally trustworthy and a reader deserves to know which they are looking at.
//
// Certain means the call went through an imported package, so the import
// statement proves what it is. Likely means it was matched on the receiver's
// name — a convention that is usually right and occasionally wrong.
const (
	Certain = "certain"
	Likely  = "likely"
)

// Advisory is one recommendation about an operation, carrying the standard it
// comes from so a reader can check it rather than take it on trust.
type Advisory struct {
	Rule      string `json:"rule"`
	Severity  string `json:"severity"` // "should" | "consider"
	Message   string `json:"message"`
	Reference string `json:"reference"`
}

// Middleware is something that runs in front of a handler. Kind and Scheme are
// inferred from the name and may be empty; Name is always what the code says.
type Middleware struct {
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`   // auth, cors, ratelimit, ...
	Scheme string `json:"scheme,omitempty"` // security scheme implied, if any

	// Headers the middleware reads from the request, and Statuses it aborts
	// with. These come from its body rather than its name, which is what makes
	// a project's own middleware documentable: a guard nobody has heard of
	// still says exactly which headers it demands and how it rejects you.
	Headers  []string `json:"headers,omitempty"`
	Statuses []int    `json:"statuses,omitempty"`
}

// Source is a position in the scanned code. Spector reads the AST, so it knows
// exactly where every operation comes from — a generator driven by annotations
// cannot know this, and a hand-written spec loses it immediately.
//
// File is relative to the scanned directory so the document stays portable
// between machines and reproducible in CI; absolute paths would leak the
// developer's home directory into a committed artifact.
type Source struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// RouteResponse is a single status-coded response an adapter discovered from a
// handler (e.g. c.JSON(201, user) or w.WriteHeader(http.StatusNotFound)).
type RouteResponse struct {
	Status int    // HTTP status code, e.g. 200, 201, 404
	Type   string // Go type name of the body, "" if no body
	Array  bool   // body is []Type
}

// Adapter scans a package's AST and returns discovered routes.
type Adapter interface {
	Name() string
	Scan(dir string) ([]Route, map[string]*Schema, []Diagnostic, error)
}
