package grpcx

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/user/specter/examples/shop/shoppb"
)

// A target that fails URL parsing is rejected by the client constructor itself.
func TestInvokeBadTargetURL(t *testing.T) {
	_, err := Invoke(protoDir, Request{
		Target: "dns:///%zz",
		Symbol: "shop.v1.UserService/GetUser",
		Data:   `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "dial") {
		t.Errorf("err = %v, want a dial error", err)
	}
}

// A server whose stream fails mid-way surfaces the status as an error, with
// the messages sent before the failure preserved in the output.
type flakyServer struct {
	shoppb.UnimplementedUserServiceServer
}

func (s *flakyServer) StreamUsers(req *shoppb.ListUsersRequest, stream shoppb.UserService_StreamUsersServer) error {
	if err := stream.Send(&shoppb.User{Id: 1, Name: "Ada"}); err != nil {
		return err
	}
	return status.Error(codes.Internal, "stream broke midway")
}

func TestInvokeStreamFailingMidwaySurfacesStatusWithPartialOutput(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	shoppb.RegisterUserServiceServer(s, &flakyServer{})
	go s.Serve(lis)
	t.Cleanup(s.Stop)

	out, err := Invoke(protoDir, Request{
		Target: lis.Addr().String(),
		Symbol: "shop.v1.UserService/StreamUsers",
		Data:   `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "stream broke midway") {
		t.Fatalf("err = %v, want the mid-stream status", err)
	}
	if !strings.Contains(out, "Ada") {
		t.Errorf("the message sent before the failure is missing:\n%s", out)
	}
}

// echoDesc is a hand-written bidi service: no generated stubs needed, the
// descriptors come from a .proto written into a temp dir.
var echoDesc = grpc.ServiceDesc{
	ServiceName: "test.Echo",
	HandlerType: (*any)(nil),
	Streams: []grpc.StreamDesc{{
		StreamName: "Chat",
		Handler: func(srv any, stream grpc.ServerStream) error {
			for {
				m := new(shoppb.GetUserRequest)
				if err := stream.RecvMsg(m); err != nil {
					return nil
				}
				if err := stream.SendMsg(m); err != nil {
					return err
				}
			}
		},
		ServerStreams: true,
		ClientStreams: true,
	}},
	Metadata: "echo.proto",
}

const echoProto = `syntax = "proto3";
package test;
message Msg { int64 id = 1; }
service Echo { rpc Chat(stream Msg) returns (stream Msg); }
`

// A bidi call whose request data goes bad after valid messages ends with the
// non-status parse error, but the responses already received are returned.
func TestInvokeBidiRequestErrorReturnsPartialOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "echo.proto"), []byte(echoProto), 0o644); err != nil {
		t.Fatal(err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	s.RegisterService(&echoDesc, struct{}{})
	go s.Serve(lis)
	t.Cleanup(s.Stop)

	var good strings.Builder
	for i := 0; i < 200; i++ {
		good.WriteString(`{"id": 1}`)
		good.WriteString("\n")
	}

	// The parse failure races the echoes coming back; retry until at least one
	// echo made it into the output before the send side gave up.
	for attempt := 0; attempt < 50; attempt++ {
		out, err := Invoke(dir, Request{
			Target: lis.Addr().String(),
			Symbol: "test.Echo/Chat",
			Data:   good.String() + `{bad json`,
		})
		if err == nil && strings.Contains(out, `"id"`) {
			return // partial output was preserved through the request error
		}
	}
	t.Error("no attempt returned partial output alongside the request error")
}
