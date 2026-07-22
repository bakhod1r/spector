package pbgo

import (
	"testing"

	"github.com/user/specter/internal/core"
)

func serviceByName(doc *core.GrpcDoc, name string) *core.GrpcService {
	for _, s := range doc.Services {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func methodByName(svc *core.GrpcService, name string) *core.GrpcMethod {
	for _, m := range svc.Methods {
		if m.Name == name {
			return m
		}
	}
	return nil
}

func TestScanGeneratedStubs(t *testing.T) {
	doc, err := Scan("../../examples/shop/shoppb")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Package != "shop.v1" {
		t.Errorf("package = %q, want shop.v1", doc.Package)
	}

	svc := serviceByName(doc, "UserService")
	if svc == nil {
		t.Fatal("UserService not found")
	}
	if svc.FullName != "shop.v1.UserService" {
		t.Errorf("fullName = %q", svc.FullName)
	}

	get := methodByName(svc, "GetUser")
	if get == nil || get.InputType != "GetUserRequest" || get.OutputType != "User" {
		t.Errorf("GetUser = %+v", get)
	}
	if get.ClientStreaming || get.ServerStreaming {
		t.Errorf("GetUser should be unary, got %+v", get)
	}

	stream := methodByName(svc, "StreamUsers")
	if stream == nil || !stream.ServerStreaming || stream.ClientStreaming {
		t.Errorf("StreamUsers should be server-streaming, got %+v", stream)
	}
	if stream.InputType != "ListUsersRequest" || stream.OutputType != "User" {
		t.Errorf("StreamUsers types = %+v", stream)
	}

	// Referenced messages are pulled into the document.
	if doc.Messages["User"] == nil || doc.Messages["GetUserRequest"] == nil {
		t.Errorf("missing referenced messages: %v", keys(doc.Messages))
	}
	// The User message struct's exported fields are captured, plumbing skipped.
	user := doc.Messages["User"]
	if user.Properties["id"] == nil || user.Properties["name"] == nil {
		t.Errorf("User properties = %+v", user.Properties)
	}
	if _, ok := user.Properties["state"]; ok {
		t.Error("pb.go plumbing field 'state' should be skipped")
	}
}

func keys(m map[string]*core.Schema) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
