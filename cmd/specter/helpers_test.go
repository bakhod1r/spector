package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageName(t *testing.T) {
	cases := map[string]string{
		"./out/Admin-Panel": "adminpanel",
		"gen":               "gen",
		"123abc":            "admin",
		"---":               "admin",
	}
	for in, want := range cases {
		if got := packageName(in); got != want {
			t.Errorf("packageName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModuleOf(t *testing.T) {
	if got := moduleOf([]byte("// c\nmodule example.com/app\n")); got != "example.com/app" {
		t.Errorf("got %q", got)
	}
	if got := moduleOf([]byte("module \"quoted.com/x\"")); got != "quoted.com/x" {
		t.Errorf("got %q", got)
	}
	if got := moduleOf([]byte("go 1.22\n")); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestImportPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "gen", "admin")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := importPath(sub); got != "example.com/app/gen/admin" {
		t.Errorf("subdir: got %q", got)
	}
	if got := importPath(root); got != "example.com/app" {
		t.Errorf("module root: got %q", got)
	}
	// go.mod without a module line yields nothing usable.
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "go.mod"), []byte("go 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := importPath(bad); got != "" {
		t.Errorf("no module line: got %q, want empty", got)
	}
}
