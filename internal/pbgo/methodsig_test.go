package pbgo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// funcType parses a method signature written as an interface member, e.g.
// "GetUser(context.Context, *Req) (*Resp, error)".
func funcType(t *testing.T, sig string) *ast.FuncType {
	t.Helper()
	src := "package p\ntype I interface {\n" + sig + "\n}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse %q: %v", sig, err)
	}
	iface := file.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec).Type.(*ast.InterfaceType)
	return iface.Methods.List[0].Type.(*ast.FuncType)
}

func TestMethodSigUnary(t *testing.T) {
	m, ok := methodSig("GetUser", funcType(t, "GetUser(context.Context, *GetUserRequest) (*User, error)"))
	if !ok {
		t.Fatal("unary signature not recognised")
	}
	if m.input != "GetUserRequest" || m.output != "User" {
		t.Errorf("in/out = %q/%q, want GetUserRequest/User", m.input, m.output)
	}
	if m.clientStream || m.serverStream {
		t.Errorf("streaming flags set on a unary method: %+v", m)
	}
}

func TestMethodSigServerStreaming(t *testing.T) {
	m, ok := methodSig("StreamUsers",
		funcType(t, "StreamUsers(*ListUsersRequest, grpc.ServerStreamingServer[User]) error"))
	if !ok {
		t.Fatal("server-streaming signature not recognised")
	}
	if !m.serverStream || m.clientStream {
		t.Errorf("flags = client:%v server:%v, want server only", m.clientStream, m.serverStream)
	}
	if m.input != "ListUsersRequest" || m.output != "User" {
		t.Errorf("in/out = %q/%q", m.input, m.output)
	}
}

func TestMethodSigClientStreaming(t *testing.T) {
	m, ok := methodSig("Upload",
		funcType(t, "Upload(grpc.ClientStreamingServer[Chunk, UploadResult]) error"))
	if !ok {
		t.Fatal("client-streaming signature not recognised")
	}
	if !m.clientStream || m.serverStream {
		t.Errorf("flags = client:%v server:%v, want client only", m.clientStream, m.serverStream)
	}
	if m.input != "Chunk" || m.output != "UploadResult" {
		t.Errorf("in/out = %q/%q, want Chunk/UploadResult", m.input, m.output)
	}
}

func TestMethodSigBidiStreaming(t *testing.T) {
	m, ok := methodSig("Chat",
		funcType(t, "Chat(grpc.BidiStreamingServer[Msg, Msg]) error"))
	if !ok {
		t.Fatal("bidi signature not recognised")
	}
	if !m.clientStream || !m.serverStream {
		t.Errorf("flags = client:%v server:%v, want both", m.clientStream, m.serverStream)
	}
	if m.input != "Msg" || m.output != "Msg" {
		t.Errorf("in/out = %q/%q", m.input, m.output)
	}
}

// Signatures that match no gRPC shape must be rejected, not guessed at.
func TestMethodSigRejectsUnknownShapes(t *testing.T) {
	cases := []string{
		"NoParams() error",
		"Three(context.Context, *Req, *Extra) error",
		"OneNonStream(*Req) error",
		"OneUnknownGeneric(grpc.SomethingElse[Req]) error",
		"OneBareGeneric(Foo[Req]) error",
	}
	for _, sig := range cases {
		t.Run(sig, func(t *testing.T) {
			if _, ok := methodSig("M", funcType(t, sig)); ok {
				t.Errorf("signature %q was accepted", sig)
			}
		})
	}
}

// A unary method with no non-error result reports an empty output rather than
// picking up "error".
func TestMethodSigUnaryWithoutResult(t *testing.T) {
	m, ok := methodSig("Fire", funcType(t, "Fire(context.Context, *Req) error"))
	if !ok {
		t.Fatal("signature not recognised")
	}
	if m.output != "" {
		t.Errorf("output = %q, want empty", m.output)
	}
}

func TestResultTypeNoResults(t *testing.T) {
	if got := resultType(funcType(t, "M(context.Context, *Req)")); got != "" {
		t.Errorf("= %q, want empty", got)
	}
}

func TestTypeArgOutOfRange(t *testing.T) {
	targs := []ast.Expr{ast.NewIdent("A")}
	if got := typeArg(targs, 0); got != "A" {
		t.Errorf("typeArg(0) = %q, want A", got)
	}
	if got := typeArg(targs, 1); got != "" {
		t.Errorf("typeArg(1) = %q, want empty", got)
	}
	if got := typeArg(targs, -1); got != "" {
		t.Errorf("typeArg(-1) = %q, want empty", got)
	}
	if got := typeArg(nil, 0); got != "" {
		t.Errorf("typeArg(nil) = %q, want empty", got)
	}
}

func TestStreamTypeRejectsNonGenerics(t *testing.T) {
	for _, src := range []string{"Req", "*Req", "[]Req", "map[string]Req"} {
		expr, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatal(err)
		}
		if kind, _ := streamType(expr); kind != "" {
			t.Errorf("streamType(%s) = %q, want empty", src, kind)
		}
	}
}

func TestFlattenParamsNil(t *testing.T) {
	if got := flattenParams(nil); got != nil {
		t.Errorf("= %+v, want nil", got)
	}
}

// Grouped parameters ("a, b *Req") expand to one entry each so positions line
// up with the gRPC shapes.
func TestFlattenParamsExpandsGroupedNames(t *testing.T) {
	ft := funcType(t, "M(ctx context.Context, a, b *Req) error")
	if got := flattenParams(ft.Params); len(got) != 3 {
		t.Errorf("= %d params, want 3", len(got))
	}
}
