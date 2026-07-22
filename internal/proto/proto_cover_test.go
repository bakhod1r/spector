package proto

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeProto(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanMissingDir(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing directory must be an error")
	}
}

func TestScanUnreadableProto(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission bits do not bind here")
	}
	dir := t.TempDir()
	writeProto(t, dir, "s.proto", `syntax = "proto3";`)
	if err := os.Chmod(filepath.Join(dir, "s.proto"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "s.proto"), 0o644) })
	if _, err := Scan(dir); err == nil {
		t.Error("an unreadable proto must be an error")
	}
}

func TestScanInvalidProto(t *testing.T) {
	dir := t.TempDir()
	writeProto(t, dir, "bad.proto", "message {{{")
	if _, err := Scan(dir); err == nil {
		t.Error("invalid proto must be an error")
	}
}

// A service option is not an RPC; a method naming an undefined message still
// scans, and a message-typed field pulls the referenced message in by ref.
func TestScanOddServiceShapes(t *testing.T) {
	dir := t.TempDir()
	writeProto(t, dir, "odd.proto", `syntax = "proto3";
package odd.v1;

message Inner { string s = 1; }
message Outer { Inner inner = 1; }

service OddService {
  option deprecated = true;
  rpc Get(Outer) returns (Missing);
}
`)
	doc, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Services) != 1 || len(doc.Services[0].Methods) != 1 {
		t.Fatalf("services = %+v", doc.Services)
	}
	if doc.Messages["Outer"] == nil || doc.Messages["Inner"] == nil {
		t.Errorf("messages = %+v, want Outer and its referenced Inner", doc.Messages)
	}
	if doc.Messages["Missing"] != nil {
		t.Error("an undefined message must not appear")
	}
}

func TestWalkNilSchemaIsIgnored(t *testing.T) {
	walk(nil, nil, map[string]bool{}) // must not panic
}
