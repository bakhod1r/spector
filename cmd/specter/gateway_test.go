package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const gatewaySrc = `syntax = "proto3";

package shop.v1;

import "google/api/annotations.proto";

service UserService {
  rpc GetUser(GetUserRequest) returns (User) {
    option (google.api.http) = {
      get: "/v1/users/{user_id}"
    };
  }
  rpc CreateUser(User) returns (User) {
    option (google.api.http) = {
      post: "/v1/users"
      body: "*"
    };
  }
  rpc Internal(User) returns (User);
}

message GetUserRequest {
  string user_id = 1;
}

message User {
  string id = 1;
  string name = 2;
}
`

func TestGatewayFlagWritesRESTDocument(t *testing.T) {
	dir := writeTree(t, map[string]string{"api.proto": gatewaySrc})
	code, stdout, stderr := exec(t, "-gateway", "-dir", dir, "-title", "Shop")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct{ Title string }
		Paths   map[string]map[string]struct {
			OperationID string          `json:"operationId"`
			RequestBody json.RawMessage `json:"requestBody"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout)
	}
	if doc.OpenAPI == "" || doc.Info.Title != "Shop" {
		t.Errorf("not an OpenAPI document with the given title: %+v", doc.Info)
	}
	if _, ok := doc.Paths["/v1/users/{user_id}"]["get"]; !ok {
		t.Errorf("annotated GET missing; paths = %v", doc.Paths)
	}
	post, ok := doc.Paths["/v1/users"]["post"]
	if !ok {
		t.Fatalf("annotated POST missing; paths = %v", doc.Paths)
	}
	if len(post.RequestBody) == 0 {
		t.Error(`body: "*" produced no request body`)
	}
	if doc.Components.Schemas["shop.v1.User"] == nil {
		t.Error("message schema not in components")
	}
	for path, methods := range doc.Paths {
		for method, op := range methods {
			if strings.HasSuffix(op.OperationID, "_Internal") {
				t.Errorf("unannotated RPC surfaced as %s %s", method, path)
			}
		}
	}
}

// -gateway honours -format like every other document.
func TestGatewayFlagYAML(t *testing.T) {
	dir := writeTree(t, map[string]string{"api.proto": gatewaySrc})
	code, stdout, stderr := exec(t, "-gateway", "-dir", dir, "-format", "yaml")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Errorf("-gateway ignored -format yaml:\n%s", stdout)
	}
}

// A proto tree with no annotations is not a failure: nothing is exposed over
// HTTP, and the run says so rather than pretending otherwise.
func TestGatewayFlagNoAnnotations(t *testing.T) {
	dir := writeTree(t, map[string]string{"api.proto": protoSrc})
	code, _, stderr := exec(t, "-gateway", "-dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "annotations") {
		t.Errorf("stderr = %q, want a warning naming the annotations", stderr)
	}
}

// Declared servers from the config file apply to the gateway document too, so
// one config describes both front doors.
func TestGatewayFlagAppliesConfig(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"api.proto":    gatewaySrc,
		"specter.yaml": "title: Shop\nservers:\n  - url: https://api.example.com\n",
	})
	code, stdout, stderr := exec(t, "-gateway", "-dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "https://api.example.com") {
		t.Errorf("declared server missing from the gateway document:\n%s", stdout)
	}
}
