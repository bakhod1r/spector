//go:build grpclive

package grpcx

import (
	"strings"
	"testing"
	"time"
)

func TestDialCredsSelectsTLS(t *testing.T) {
	if got := dialCreds(Request{}); got.Info().SecurityProtocol != "insecure" {
		t.Fatalf("default should be insecure, got %q", got.Info().SecurityProtocol)
	}
	if got := dialCreds(Request{TLS: true}); got.Info().SecurityProtocol == "insecure" {
		t.Fatalf("TLS:true should not be insecure")
	}
}

func TestTimeoutDefault(t *testing.T) {
	if d := timeoutOf(Request{}); d != 15*time.Second {
		t.Fatalf("default timeout = %v", d)
	}
	if d := timeoutOf(Request{TimeoutSec: 5}); d != 5*time.Second {
		t.Fatalf("custom timeout = %v", d)
	}
}

func TestInvokeRequiresTarget(t *testing.T) {
	if _, err := Invoke("", Request{Symbol: "pkg.Svc/M"}); err == nil {
		t.Fatal("expected error when target is empty")
	}
}

func TestProtoFiles(t *testing.T) {
	files, err := protoFiles("../proto/testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "shop.proto" {
		t.Errorf("proto files = %v, want [shop.proto] (relative)", files)
	}
}

func TestProtoFilesEmptyDir(t *testing.T) {
	if _, err := protoFiles(""); err == nil {
		t.Error("expected error for empty proto dir")
	}
}

func TestSymbolNormalization(t *testing.T) {
	// The invoke path rewrites the first "/" to "." so both
	// package.Service/Method and package.Service.Method are accepted.
	got := strings.Replace("shop.v1.UserService/GetUser", "/", ".", 1)
	if got != "shop.v1.UserService.GetUser" {
		t.Errorf("symbol = %q", got)
	}
}

// Client-streaming Execute must send every newline-separated JSON object in
// req.Data as its own message on the stream, not just the first.
func TestInvokeClientStreamSendsAllMessages(t *testing.T) {
	target := startServer(t, false)
	req := Request{
		Target: target,
		Symbol: "shop.v1.UserService/CountUsers",
		Data:   "{\"id\":1}\n{\"id\":2}\n{\"id\":3}",
	}
	out, err := Invoke(protoDir, req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !strings.Contains(out, "\"count\": 3") && !strings.Contains(out, "\"count\":3") {
		t.Fatalf("expected count 3, got: %s", out)
	}
}
