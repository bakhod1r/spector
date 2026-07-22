package graphqlsdl

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestScanMissingDir(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing directory must be an error")
	}
}

func TestScanEmptyDirGivesEmptyDoc(t *testing.T) {
	doc, err := Scan(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Queries) != 0 || len(doc.Types) != 0 {
		t.Errorf("empty dir gave %+v", doc)
	}
}

func TestScanUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission bits do not bind here")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "s.graphql")
	if err := os.WriteFile(path, []byte("type Query { a: Int }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if _, err := Scan(dir); err == nil {
		t.Error("an unreadable schema file must be an error")
	}
}

func TestScanInvalidSDL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.graphqls"), []byte("type {{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(dir); err == nil {
		t.Error("invalid SDL must be an error")
	}
}

func TestScanSubscriptionRoot(t *testing.T) {
	dir := t.TempDir()
	sdl := "type Subscription {\n  ticks(interval: Int): String\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "sub.graphql"), []byte(sdl), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Subscriptions) != 1 || doc.Subscriptions[0].Name != "ticks" {
		t.Errorf("subscriptions = %+v", doc.Subscriptions)
	}
}
