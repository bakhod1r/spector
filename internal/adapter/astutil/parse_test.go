package astutil

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestParseDirWalksSubdirectoriesAndSkipsTests(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "package app\n")
	writeFile(t, filepath.Join(dir, "main_test.go"), "package app\n")
	writeFile(t, filepath.Join(dir, "internal", "http", "routes.go"), "package http\n")
	writeFile(t, filepath.Join(dir, "vendor", "dep", "dep.go"), "package dep\n")
	writeFile(t, filepath.Join(dir, ".hidden", "hidden.go"), "package hidden\n")

	files, err := ParseDir(token.NewFileSet(), dir, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name.Name)
	}
	// Sorted by path, so internal/http/routes.go comes before main.go.
	if got := strings.Join(names, ","); got != "http,app" {
		t.Errorf("packages = %q, want \"http,app\"", got)
	}
}

// One unbuildable package in a large tree must not cost the whole document.
func TestParseDirSkipsUnparseableFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "good.go"), "package app\n")
	writeFile(t, filepath.Join(dir, "bad", "bad.go"), "package {\n")

	files, err := ParseDir(token.NewFileSet(), dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want the one that parses", len(files))
	}
}

// When nothing parsed, the parse error is the only thing worth reporting.
func TestParseDirReportsErrorWhenNothingParses(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.go"), "package {\n")
	if _, err := ParseDir(token.NewFileSet(), dir, 0); err == nil {
		t.Error("want an error when every file is unparseable")
	}
}

func TestParseDirReportsMissingDir(t *testing.T) {
	if _, err := ParseDir(token.NewFileSet(), filepath.Join(t.TempDir(), "nope"), 0); err == nil {
		t.Error("want an error for a directory that does not exist")
	}
}
