//go:build grpclive

package specter

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bakhod1r/spector/examples/shop/shoppb"
	"google.golang.org/grpc"
)

// ---- successful gRPC invoke ----

type testUserServer struct {
	shoppb.UnimplementedUserServiceServer
}

func (s *testUserServer) GetUser(ctx context.Context, req *shoppb.GetUserRequest) (*shoppb.User, error) {
	return &shoppb.User{Id: req.Id, Name: "Ada", Email: "ada@example.com"}, nil
}

// startTestGRPC brings up a real gRPC server on an ephemeral port; the invoke
// path resolves descriptors from the .proto files, so no reflection is needed.
func startTestGRPC(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	shoppb.RegisterUserServiceServer(s, &testUserServer{})
	go s.Serve(lis)
	t.Cleanup(s.Stop)
	return lis.Addr().String()
}

// The whole point of the console's Execute button: a real call must come back
// as the decoded response body, not merely "no error".
func TestHandlerGrpcInvokeSucceeds(t *testing.T) {
	target := startTestGRPC(t)
	h := Handler(Config{Dir: "examples/shop", ProtoDir: "examples/shop/proto"})

	body := `{"target":"` + target + `","symbol":"shop.v1.UserService/GetUser","data":"{\"id\":7}"}`
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/docs/grpc/invoke", strings.NewReader(body)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v\nbody: %s", err, w.Body.String())
	}
	if got["name"] != "Ada" {
		t.Errorf("name = %v, want Ada (body: %s)", got["name"], w.Body.String())
	}
}
