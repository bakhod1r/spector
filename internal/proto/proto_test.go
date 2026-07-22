package proto

import (
	"testing"

	"github.com/user/specter/internal/core"
)

func TestScan(t *testing.T) {
	doc, err := Scan("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Package != "shop.v1" {
		t.Errorf("package = %q, want shop.v1", doc.Package)
	}
	if len(doc.Services) != 1 || doc.Services[0].Name != "UserService" {
		t.Fatalf("services = %+v", doc.Services)
	}

	svc := doc.Services[0]
	if svc.FullName != "shop.v1.UserService" {
		t.Errorf("fullName = %q", svc.FullName)
	}

	var list *core.GrpcMethod
	for _, m := range svc.Methods {
		if m.Name == "ListUsers" {
			list = m
		}
	}
	if list == nil || !list.ServerStreaming {
		t.Errorf("ListUsers should be server-streaming: %+v", list)
	}

	// Enum variant names are surfaced as the schema's enum values.
	status := doc.Messages["Status"]
	if status == nil {
		t.Fatal("Status enum message missing")
	}
	if len(status.Enum) != 3 || status.Enum[0] != "STATUS_UNKNOWN" || status.Enum[2] != "STATUS_SUSPENDED" {
		t.Errorf("Status enum = %v", status.Enum)
	}

	// Repeated + map fields map to array / object schemas.
	user := doc.Messages["User"]
	if user == nil {
		t.Fatal("User message missing")
	}
	if roles := user.Properties["roles"]; roles == nil || roles.Type != "array" {
		t.Errorf("roles = %+v", roles)
	}
	if md := user.Properties["metadata"]; md == nil || md.AdditionalProperties == nil {
		t.Errorf("metadata = %+v", md)
	}
}
