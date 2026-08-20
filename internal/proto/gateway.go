package proto

import (
	"os"
	"sort"
	"strings"

	"github.com/bakhod1r/spector/internal/core"
	"github.com/emicklei/proto"
)

// httpOption is the annotation gRPC-Gateway (and Google's own API tooling)
// reads to map an RPC onto a REST route.
const httpOption = "(google.api.http)"

// ScanGateway builds a REST document from google.api.http annotations in the
// .proto files under dir. An RPC without the annotation is gRPC-only and does
// not appear: the document describes what the gateway actually exposes over
// HTTP, not every method on the service.
func ScanGateway(dir string) (*core.Document, error) {
	files, err := protoFiles(dir)
	if err != nil {
		return nil, err
	}
	doc := core.NewDocument("", "")
	all := map[string]*core.Schema{}

	// bindings are collected first so every message in every file is known
	// before a binding refers to one.
	type binding struct {
		service string
		rpc     *proto.RPC
		pkg     string
		method  string // lowercase HTTP method
		path    string // the raw path template
		body    string // "", "*", or a field name
	}
	var bindings []binding

	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		def, perr := proto.NewParser(f).Parse()
		// Closing a file opened for reading reports nothing a caller can act
		// on: the parse above already consumed it, or already failed.
		_ = f.Close()
		if perr != nil {
			return nil, perr
		}

		var pkg string
		proto.Walk(def, proto.WithPackage(func(p *proto.Package) { pkg = p.Name }))
		filePkg := pkg

		proto.Walk(def,
			proto.WithMessage(func(m *proto.Message) { all[qualify(m.Name, filePkg)] = messageToSchema(m, filePkg) }),
			proto.WithEnum(func(e *proto.Enum) { all[qualify(e.Name, filePkg)] = enumToSchema(e) }),
			proto.WithService(func(s *proto.Service) {
				for _, e := range s.Elements {
					rpc, ok := e.(*proto.RPC)
					if !ok {
						continue
					}
					for _, rule := range httpRules(rpc) {
						bindings = append(bindings, binding{
							service: s.Name,
							rpc:     rpc,
							pkg:     filePkg,
							method:  rule.method,
							path:    rule.path,
							body:    rule.body,
						})
					}
				}
			}),
		)
	}

	used := map[string]bool{}
	for _, b := range bindings {
		in := qualify(b.rpc.RequestType, b.pkg)
		out := qualify(b.rpc.ReturnsType, b.pkg)
		path, params := templateParams(b.path, all[in])
		op := &core.Operation{
			OperationID: b.service + "_" + b.rpc.Name,
			Tags:        []string{b.service},
			Parameters:  params,
			Responses: map[string]*core.Response{
				"200": {
					Description: "OK",
					Content:     map[string]core.MediaType{"application/json": {Schema: refTo(out)}},
				},
			},
		}
		collect(out, all, used)

		switch {
		case b.body == "*":
			op.RequestBody = &core.RequestBody{
				Required: true,
				Content:  map[string]core.MediaType{"application/json": {Schema: refTo(in)}},
			}
			collect(in, all, used)
		case b.body != "":
			// A named body field: the body is that field's own type.
			if schema := fieldRef(all[in], b.body); schema != nil {
				op.RequestBody = &core.RequestBody{
					Required: true,
					Content:  map[string]core.MediaType{"application/json": {Schema: schema}},
				}
				if schema.Ref != "" {
					collect(strings.TrimPrefix(schema.Ref, "#/components/schemas/"), all, used)
				}
			}
		default:
			// No body: every request field that the path did not consume is
			// read from the query string, which is what the gateway does.
			op.Parameters = append(op.Parameters, queryParams(all[in], params)...)
		}

		// Server streaming over the gateway is a stream of JSON messages, not
		// one body. Saying so keeps the document from describing it as an
		// ordinary call that returns a single object.
		if b.rpc.StreamsReturns {
			op.Realtime = "stream"
		}

		doc.AddOperation(path, b.method, op)
	}

	for name := range used {
		if s := all[name]; s != nil {
			doc.Components.Schemas[name] = s
		}
	}
	return doc, nil
}

// httpRule is one method+path+body triple: the annotation's own rule, plus
// each additional_bindings entry.
type httpRule struct {
	method string
	path   string
	body   string
}

// httpMethods are the annotation keys that name an HTTP method directly.
// "custom" carries its own kind and is handled separately.
var httpMethods = map[string]bool{"get": true, "put": true, "post": true, "delete": true, "patch": true}

// httpRules reads every rule out of an RPC's (google.api.http) option.
func httpRules(rpc *proto.RPC) []httpRule {
	var out []httpRule
	for _, e := range rpc.Elements {
		opt, ok := e.(*proto.Option)
		if !ok || opt.Name != httpOption {
			continue
		}
		out = append(out, rulesFromLiteral(&opt.Constant)...)
	}
	return out
}

// rulesFromLiteral turns one HttpRule literal into rules: the rule itself,
// then any additional_bindings nested inside it.
func rulesFromLiteral(lit *proto.Literal) []httpRule {
	var rules []httpRule
	rule := httpRule{}
	var nested []httpRule

	for _, entry := range lit.OrderedMap {
		name := strings.Trim(entry.Name, `"`)
		switch {
		case httpMethods[name]:
			rule.method, rule.path = name, entry.Literal.Source
		case name == "body":
			rule.body = entry.Literal.Source
		case name == "custom":
			// custom { kind: "OPTIONS" path: "/v1/x" }
			var kind, path string
			for _, sub := range entry.Literal.OrderedMap {
				switch strings.Trim(sub.Name, `"`) {
				case "kind":
					kind = sub.Literal.Source
				case "path":
					path = sub.Literal.Source
				}
			}
			if kind != "" && path != "" {
				rule.method, rule.path = strings.ToLower(kind), path
			}
		case name == "additional_bindings":
			nested = append(nested, rulesFromLiteral(entry.Literal)...)
		}
	}
	if rule.method != "" && rule.path != "" {
		rules = append(rules, rule)
	}
	return append(rules, nested...)
}

// templateParams rewrites a gateway path template into an OpenAPI path and
// returns the path parameters it declares. "{name=users/*}" names the field
// "name": the pattern after "=" is a gateway matching detail with no OpenAPI
// equivalent, so it is dropped rather than smuggled into the path.
func templateParams(template string, in *core.Schema) (string, []core.Parameter) {
	var params []core.Parameter
	var b strings.Builder
	rest := template
	for {
		open := strings.Index(rest, "{")
		if open < 0 {
			b.WriteString(rest)
			break
		}
		close := strings.Index(rest[open:], "}")
		if close < 0 {
			b.WriteString(rest)
			break
		}
		close += open
		name := rest[open+1 : close]
		if i := strings.Index(name, "="); i >= 0 {
			name = name[:i]
		}
		name = strings.TrimSpace(name)
		b.WriteString(rest[:open])
		b.WriteString("{" + name + "}")
		params = append(params, core.Parameter{
			Name:     name,
			In:       "path",
			Required: true,
			Schema:   fieldSchemaOrString(in, name),
		})
		rest = rest[close+1:]
	}
	return b.String(), params
}

// queryParams turns every request field the path did not consume into a query
// parameter. Message-typed fields are skipped: the gateway flattens them with
// dotted names, which is more shape than a parameter list can honestly carry.
func queryParams(in *core.Schema, taken []core.Parameter) []core.Parameter {
	if in == nil {
		return nil
	}
	inPath := map[string]bool{}
	for _, p := range taken {
		inPath[p.Name] = true
	}
	var out []core.Parameter
	for _, name := range sortedSchemaKeys(in.Properties) {
		if inPath[name] {
			continue
		}
		s := in.Properties[name]
		if s == nil || s.Ref != "" {
			continue
		}
		out = append(out, core.Parameter{Name: name, In: "query", Schema: s})
	}
	return out
}

// fieldRef returns the schema for one field of a message, which is what a
// `body: "field"` binding sends.
func fieldRef(in *core.Schema, field string) *core.Schema {
	if in == nil {
		return nil
	}
	return in.Properties[field]
}

// fieldSchemaOrString gives a path parameter the field's own schema, falling
// back to string — a path segment is a string when nothing better is known.
func fieldSchemaOrString(in *core.Schema, field string) *core.Schema {
	if s := fieldRef(in, field); s != nil && s.Ref == "" {
		return s
	}
	return &core.Schema{Type: "string"}
}

func refTo(name string) *core.Schema {
	return &core.Schema{Ref: "#/components/schemas/" + name}
}

// sortedSchemaKeys keeps query-parameter order stable across runs; map
// iteration order would otherwise reshuffle the document on every scan.
func sortedSchemaKeys(m map[string]*core.Schema) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
