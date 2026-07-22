package proto

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/emicklei/proto"
	"github.com/user/specter/internal/core"
)

func Scan(dir string) (*core.GrpcDoc, error) {
	files, err := protoFiles(dir)
	if err != nil {
		return nil, err
	}
	doc := core.NewGrpcDoc()
	all := map[string]*core.Schema{}

	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		def, err := proto.NewParser(f).Parse()
		f.Close()
		if err != nil {
			return nil, err
		}

		var pkg string
		proto.Walk(def,
			proto.WithPackage(func(p *proto.Package) { pkg = p.Name }),
		)
		if pkg != "" && doc.Package == "" {
			doc.Package = pkg
		}
		proto.Walk(def,
			proto.WithMessage(func(m *proto.Message) { all[m.Name] = messageToSchema(m) }),
			proto.WithEnum(func(e *proto.Enum) { all[e.Name] = enumToSchema(e) }),
			proto.WithService(func(s *proto.Service) {
				doc.Services = append(doc.Services, serviceToGrpc(pkg, s))
			}),
		)
	}

	used := map[string]bool{}
	for _, svc := range doc.Services {
		for _, m := range svc.Methods {
			collect(m.InputType, all, used)
			collect(m.OutputType, all, used)
		}
	}
	for name := range used {
		if s := all[name]; s != nil {
			doc.Messages[name] = s
		}
	}
	return doc, nil
}

func protoFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".proto") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func serviceToGrpc(pkg string, s *proto.Service) *core.GrpcService {
	full := s.Name
	if pkg != "" {
		full = pkg + "." + s.Name
	}
	svc := &core.GrpcService{Name: s.Name, FullName: full}
	for _, e := range s.Elements {
		rpc, ok := e.(*proto.RPC)
		if !ok {
			continue
		}
		svc.Methods = append(svc.Methods, &core.GrpcMethod{
			Name:            rpc.Name,
			InputType:       rpc.RequestType,
			OutputType:      rpc.ReturnsType,
			ClientStreaming: rpc.StreamsRequest,
			ServerStreaming: rpc.StreamsReturns,
		})
	}
	return svc
}

func messageToSchema(m *proto.Message) *core.Schema {
	schema := &core.Schema{Type: "object", Properties: map[string]*core.Schema{}}
	for _, e := range m.Elements {
		switch f := e.(type) {
		case *proto.NormalField:
			schema.Properties[f.Name] = fieldSchema(f.Type, f.Repeated)
		case *proto.MapField:
			schema.Properties[f.Name] = &core.Schema{
				Type:                 "object",
				AdditionalProperties: fieldSchema(f.Type, false),
			}
		}
	}
	return schema
}

// enumToSchema turns a proto enum into a string schema carrying its variant
// names as the allowed enum values (e.g. UNKNOWN, ACTIVE, SUSPENDED).
func enumToSchema(e *proto.Enum) *core.Schema {
	schema := &core.Schema{Type: "string"}
	for _, el := range e.Elements {
		if field, ok := el.(*proto.EnumField); ok {
			schema.Enum = append(schema.Enum, field.Name)
		}
	}
	return schema
}

func fieldSchema(protoType string, repeated bool) *core.Schema {
	base := scalarSchema(protoType)
	if repeated {
		return &core.Schema{Type: "array", Items: base}
	}
	return base
}

func scalarSchema(t string) *core.Schema {
	switch t {
	case "string":
		return &core.Schema{Type: "string"}
	case "bool":
		return &core.Schema{Type: "boolean"}
	case "bytes":
		return &core.Schema{Type: "string", Format: "byte"}
	case "double", "float":
		return &core.Schema{Type: "number"}
	case "int32", "int64", "uint32", "uint64", "sint32", "sint64",
		"fixed32", "fixed64", "sfixed32", "sfixed64":
		return &core.Schema{Type: "integer"}
	default:
		return &core.Schema{Ref: "#/components/schemas/" + t}
	}
}

func collect(name string, all map[string]*core.Schema, used map[string]bool) {
	if name == "" || used[name] {
		return
	}
	schema := all[name]
	if schema == nil {
		return
	}
	used[name] = true
	walk(schema, all, used)
}

func walk(schema *core.Schema, all map[string]*core.Schema, used map[string]bool) {
	if schema == nil {
		return
	}
	if schema.Ref != "" {
		collect(strings.TrimPrefix(schema.Ref, "#/components/schemas/"), all, used)
	}
	if schema.Items != nil {
		walk(schema.Items, all, used)
	}
	if schema.AdditionalProperties != nil {
		walk(schema.AdditionalProperties, all, used)
	}
	for _, p := range schema.Properties {
		walk(p, all, used)
	}
}
