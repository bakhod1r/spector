package grpcx

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"github.com/user/specter/examples/shop/shoppb"
)

const protoDir = "../../examples/shop/proto"

type userServer struct {
	shoppb.UnimplementedUserServiceServer
}

func (s *userServer) GetUser(ctx context.Context, req *shoppb.GetUserRequest) (*shoppb.User, error) {
	return &shoppb.User{Id: req.Id, Name: "Ada", Email: "ada@example.com", Roles: []string{"admin"}}, nil
}

func (s *userServer) ListUsers(ctx context.Context, req *shoppb.ListUsersRequest) (*shoppb.ListUsersResponse, error) {
	return &shoppb.ListUsersResponse{Users: []*shoppb.User{{Id: 1, Name: "Ada"}, {Id: 2, Name: "Alan"}}}, nil
}

func (s *userServer) StreamUsers(req *shoppb.ListUsersRequest, stream shoppb.UserService_StreamUsersServer) error {
	for _, u := range []*shoppb.User{{Id: 1, Name: "Ada"}, {Id: 2, Name: "Alan"}} {
		if err := stream.Send(u); err != nil {
			return err
		}
	}
	return nil
}

// startServer brings up a real gRPC server on an ephemeral port. withReflection
// controls whether the reflection service is registered, which is what decides
// the descriptor source when no .proto files are available.
func startServer(t *testing.T, withReflection bool) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	shoppb.RegisterUserServiceServer(s, &userServer{})
	if withReflection {
		reflection.Register(s)
	}
	go s.Serve(lis)
	t.Cleanup(s.Stop)
	return lis.Addr().String()
}

func TestInvokeUnary(t *testing.T) {
	target := startServer(t, false)
	out, err := Invoke(protoDir, Request{
		Target: target,
		Symbol: "shop.v1.UserService/GetUser",
		Data:   `{"id": 7}`,
	})
	if err != nil {
		t.Fatalf("Invoke: %v (out: %s)", err, out)
	}
	for _, want := range []string{`"id": 7`, `"name": "Ada"`} {
		if !strings.Contains(out, want) {
			t.Errorf("response missing %s; got:\n%s", want, out)
		}
	}
}

// The console sends the symbol with a slash between service and method; the
// invoke path has to normalise that into a fully-qualified name.
func TestInvokeAcceptsSlashSymbol(t *testing.T) {
	target := startServer(t, false)
	if _, err := Invoke(protoDir, Request{
		Target: target,
		Symbol: "shop.v1.UserService/ListUsers",
		Data:   `{}`,
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
}

// Server-streaming responses arrive as several messages; all of them must make
// it into the output rather than just the first.
func TestInvokeServerStreaming(t *testing.T) {
	target := startServer(t, false)
	out, err := Invoke(protoDir, Request{
		Target: target,
		Symbol: "shop.v1.UserService/StreamUsers",
		Data:   `{}`,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.Contains(out, "Ada") || !strings.Contains(out, "Alan") {
		t.Errorf("expected both streamed users; got:\n%s", out)
	}
}

func TestInvokeSendsHeaders(t *testing.T) {
	target := startServer(t, false)
	if _, err := Invoke(protoDir, Request{
		Target:  target,
		Symbol:  "shop.v1.UserService/GetUser",
		Data:    `{"id": 1}`,
		Headers: map[string]string{"authorization": "Bearer t"},
	}); err != nil {
		t.Fatalf("Invoke with headers: %v", err)
	}
}

// With no .proto files to read, descriptors have to come from the server's
// reflection service instead.
func TestInvokeFallsBackToReflection(t *testing.T) {
	target := startServer(t, true)
	out, err := Invoke(t.TempDir(), Request{
		Target: target,
		Symbol: "shop.v1.UserService/GetUser",
		Data:   `{"id": 3}`,
	})
	if err != nil {
		t.Fatalf("Invoke via reflection: %v (out: %s)", err, out)
	}
	if !strings.Contains(out, "Ada") {
		t.Errorf("reflection-based invoke returned:\n%s", out)
	}
}

// Without protos *and* without reflection there is no way to describe the
// service, so this must fail rather than hang or panic.
func TestInvokeNoDescriptorsFails(t *testing.T) {
	target := startServer(t, false)
	if _, err := Invoke(t.TempDir(), Request{
		Target: target,
		Symbol: "shop.v1.UserService/GetUser",
		Data:   `{}`,
	}); err == nil {
		t.Error("expected an error with neither protos nor reflection")
	}
}

// An unparseable .proto must abort the call: silently falling through to
// reflection would hide the fact that the user's schema is broken.
func TestInvokeBadProtoDirFails(t *testing.T) {
	target := startServer(t, true)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.proto"), []byte("this is not proto"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Invoke(dir, Request{
		Target: target,
		Symbol: "shop.v1.UserService/GetUser",
		Data:   `{}`,
	}); err == nil {
		t.Error("expected an error when the proto dir cannot be parsed")
	}
}

func TestInvokeUnknownMethod(t *testing.T) {
	target := startServer(t, false)
	_, err := Invoke(protoDir, Request{
		Target: target,
		Symbol: "shop.v1.UserService/NoSuchMethod",
		Data:   `{}`,
	})
	if err == nil {
		t.Fatal("expected an error for an unknown method")
	}
	if !strings.Contains(err.Error(), "NoSuchMethod") {
		t.Errorf("error should name the missing method; got %v", err)
	}
}

func TestInvokeMalformedRequestJSON(t *testing.T) {
	target := startServer(t, false)
	if _, err := Invoke(protoDir, Request{
		Target: target,
		Symbol: "shop.v1.UserService/GetUser",
		Data:   `{not json`,
	}); err == nil {
		t.Error("expected an error for malformed request JSON")
	}
}

func TestInvokeUnreachableTarget(t *testing.T) {
	if _, err := Invoke(protoDir, Request{
		Target: "127.0.0.1:1",
		Symbol: "shop.v1.UserService/GetUser",
		Data:   `{}`,
	}); err == nil {
		t.Error("expected an error for an unreachable target")
	}
}

// A method that returns a non-OK status must surface that status, not be
// reported as a success with an empty body.
func TestInvokeUnimplementedMethodSurfacesStatus(t *testing.T) {
	target := startServer(t, false)
	_, err := Invoke(protoDir, Request{
		Target: target,
		Symbol: "shop.v1.UserService/DeleteUser",
		Data:   `{"id": 1}`,
	})
	if err == nil {
		t.Fatal("expected a status error from an unimplemented method")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unimplemented") {
		t.Errorf("error should mention the status; got %v", err)
	}
}

func TestDescriptorSourceFromProtoFiles(t *testing.T) {
	ctx := context.Background()
	cc, err := grpc.NewClient("127.0.0.1:1", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()

	src, err := descriptorSource(ctx, protoDir, cc)
	if err != nil {
		t.Fatalf("descriptorSource: %v", err)
	}
	if _, err := src.FindSymbol("shop.v1.UserService"); err != nil {
		t.Errorf("UserService not found in descriptor source: %v", err)
	}
}

// A directory holding an unparseable .proto must not be treated as a usable
// descriptor source; the error is reported alongside the reflection fallback.
func TestDescriptorSourceBadProtoReportsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.proto"), []byte("this is not proto"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cc, err := grpc.NewClient("127.0.0.1:1", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()

	if _, err := descriptorSource(ctx, dir, cc); err == nil {
		t.Error("expected an error for an unparseable .proto")
	}
}

func TestProtoFilesNested(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "deep.proto"), []byte("syntax = \"proto3\";"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := protoFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %v, want exactly the one .proto", files)
	}
	// Paths are returned relative to dir so the import resolver can find them.
	if filepath.IsAbs(files[0]) {
		t.Errorf("files[0] = %q, want a path relative to dir", files[0])
	}
}

func TestProtoFilesMissingDir(t *testing.T) {
	if _, err := protoFiles(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected an error for a missing dir")
	}
}
