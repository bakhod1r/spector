// Package pbgo reads gRPC service definitions from generated Go code
// (protoc-gen-go / protoc-gen-go-grpc output: *.pb.go and *_grpc.pb.go)
// rather than from .proto sources. It is the fallback used when a project
// ships generated stubs but not the original protos.
package pbgo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/specter/internal/adapter/astutil"
	"github.com/user/specter/internal/core"
)

const refPrefix = "#/components/schemas/"

// Scan walks dir for *.pb.go files and reconstructs a gRPC document from the
// generated ServiceDesc values, server interfaces, and message structs.
func Scan(dir string) (*core.GrpcDoc, error) {
	files, err := parseFiles(dir)
	if err != nil {
		return nil, err
	}

	scanner := core.NewStructScanner()
	ifaces := map[string]serverIface{} // interface name -> parsed methods
	descs := []serviceDesc{}
	for _, file := range files {
		scanner.Collect(file)
		collectInterfaces(file, ifaces)
		descs = append(descs, collectServiceDescs(file)...)
	}

	doc := core.NewGrpcDoc()
	used := map[string]bool{}
	for _, d := range descs {
		svc := &core.GrpcService{Name: shortName(d.fullName), FullName: d.fullName}
		if doc.Package == "" {
			doc.Package = pkgOf(d.fullName)
		}
		for _, m := range ifaces[d.iface].methods {
			svc.Methods = append(svc.Methods, &core.GrpcMethod{
				Name:            m.name,
				InputType:       m.input,
				OutputType:      m.output,
				ClientStreaming: m.clientStream,
				ServerStreaming: m.serverStream,
			})
			collect(m.input, scanner.Schemas, used)
			collect(m.output, scanner.Schemas, used)
		}
		doc.Services = append(doc.Services, svc)
	}

	for name := range used {
		if s := scanner.Schemas[name]; s != nil {
			doc.Messages[name] = s
		}
	}
	return doc, nil
}

func parseFiles(dir string) ([]*ast.File, error) {
	var files []*ast.File
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".pb.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		files = append(files, f)
		return nil
	})
	return files, err
}

// serverIface is a parsed XxxServer interface: its RPC method signatures.
type serverIface struct {
	methods []ifaceMethod
}

type ifaceMethod struct {
	name         string
	input        string
	output       string
	clientStream bool
	serverStream bool
}

// collectInterfaces records every "XxxServer" interface with the RPC methods
// it declares (skipping the mustEmbed/testEmbedded plumbing methods).
func collectInterfaces(file *ast.File, out map[string]serverIface) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !strings.HasSuffix(ts.Name.Name, "Server") {
				continue
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				continue
			}
			var iface serverIface
			for _, field := range it.Methods.List {
				if len(field.Names) == 0 || !field.Names[0].IsExported() {
					continue // embedded interface or plumbing method
				}
				ft, ok := field.Type.(*ast.FuncType)
				if !ok {
					continue
				}
				if m, ok := methodSig(field.Names[0].Name, ft); ok {
					iface.methods = append(iface.methods, m)
				}
			}
			out[ts.Name.Name] = iface
		}
	}
}

// methodSig extracts input/output types and streaming kind from a generated
// server method signature. Supported shapes:
//
//	Unary:  M(context.Context, *Req) (*Resp, error)
//	Server: M(*Req, grpc.ServerStreamingServer[Resp]) error
//	Client: M(grpc.ClientStreamingServer[Req, Resp]) error
//	Bidi:   M(grpc.BidiStreamingServer[Req, Resp]) error
func methodSig(name string, ft *ast.FuncType) (ifaceMethod, bool) {
	m := ifaceMethod{name: name}
	params := flattenParams(ft.Params)
	switch len(params) {
	case 2:
		if kind, targs := streamType(params[1]); kind == "server" {
			m.serverStream = true
			m.input = astutil.TypeName(params[0]).Name
			m.output = typeArg(targs, 0)
			return m, true
		}
		// Unary: (ctx, *Req) (*Resp, error).
		m.input = astutil.TypeName(params[1]).Name
		m.output = resultType(ft)
		return m, true
	case 1:
		kind, targs := streamType(params[0])
		switch kind {
		case "client":
			m.clientStream = true
		case "bidi":
			m.clientStream, m.serverStream = true, true
		default:
			return m, false
		}
		m.input = typeArg(targs, 0)
		m.output = typeArg(targs, 1)
		return m, true
	}
	return m, false
}

func flattenParams(fl *ast.FieldList) []ast.Expr {
	if fl == nil {
		return nil
	}
	var out []ast.Expr
	for _, f := range fl.List {
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			out = append(out, f.Type)
		}
	}
	return out
}

// streamType reports the grpc streaming helper a parameter references and its
// generic type arguments: grpc.ServerStreamingServer[Resp] etc.
func streamType(e ast.Expr) (kind string, targs []ast.Expr) {
	var x ast.Expr
	switch t := e.(type) {
	case *ast.IndexExpr:
		x, targs = t.X, []ast.Expr{t.Index}
	case *ast.IndexListExpr:
		x, targs = t.X, t.Indices
	default:
		return "", nil
	}
	sel, ok := x.(*ast.SelectorExpr)
	if !ok {
		return "", nil
	}
	switch sel.Sel.Name {
	case "ServerStreamingServer":
		return "server", targs
	case "ClientStreamingServer":
		return "client", targs
	case "BidiStreamingServer":
		return "bidi", targs
	}
	return "", nil
}

func typeArg(targs []ast.Expr, i int) string {
	if i < 0 || i >= len(targs) {
		return ""
	}
	return astutil.TypeName(targs[i]).Name
}

// resultType returns the first non-error result type name of a signature.
func resultType(ft *ast.FuncType) string {
	if ft.Results == nil {
		return ""
	}
	for _, r := range ft.Results.List {
		if n := astutil.TypeName(r.Type).Name; n != "" && n != "error" {
			return n
		}
	}
	return ""
}

// serviceDesc is the essential data from a generated grpc.ServiceDesc value:
// the fully-qualified service name and the server interface it binds to.
type serviceDesc struct {
	fullName string
	iface    string
}

// collectServiceDescs finds `var X = grpc.ServiceDesc{...}` values and pulls
// out ServiceName and the interface named by HandlerType: (*XServer)(nil).
func collectServiceDescs(file *ast.File) []serviceDesc {
	var out []serviceDesc
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, val := range vs.Values {
				lit, ok := val.(*ast.CompositeLit)
				if !ok || !isServiceDesc(lit.Type) {
					continue
				}
				if d, ok := parseServiceDesc(lit); ok {
					out = append(out, d)
				}
			}
		}
	}
	return out
}

func isServiceDesc(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "ServiceDesc"
}

func parseServiceDesc(lit *ast.CompositeLit) (serviceDesc, bool) {
	var d serviceDesc
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "ServiceName":
			if s, ok := astutil.StringLit(kv.Value); ok {
				d.fullName = s
			}
		case "HandlerType":
			d.iface = handlerIface(kv.Value)
		}
	}
	return d, d.fullName != ""
}

// handlerIface resolves the interface name from `(*UserServiceServer)(nil)`.
func handlerIface(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	paren, ok := call.Fun.(*ast.ParenExpr)
	if !ok {
		return ""
	}
	star, ok := paren.X.(*ast.StarExpr)
	if !ok {
		return ""
	}
	if id, ok := star.X.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// shortName returns the final segment of a dotted service name.
func shortName(full string) string {
	if i := strings.LastIndex(full, "."); i != -1 {
		return full[i+1:]
	}
	return full
}

// pkgOf returns everything before the final segment of a dotted service name.
func pkgOf(full string) string {
	if i := strings.LastIndex(full, "."); i != -1 {
		return full[:i]
	}
	return ""
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
		collect(strings.TrimPrefix(schema.Ref, refPrefix), all, used)
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
