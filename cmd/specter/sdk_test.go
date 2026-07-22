package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The example app is scanned rather than a synthetic one-route file: it has
// request and response types, so the generated client exercises schemas as
// well as method signatures.
const exampleDir = "../../examples/ginapp"

func TestRunSDKTypeScript(t *testing.T) {
	out := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"-dir", exampleDir, "-sdk", "ts", "-sdk-out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run = %d: %s", code, stderr.String())
	}

	data, err := os.ReadFile(filepath.Join(out, "client.ts"))
	if err != nil {
		t.Fatalf("client.ts not written: %v", err)
	}
	src := string(data)
	for _, want := range []string{
		"export class Client",
		"export interface User {",
		"export interface CreateUserReq {",
		// The document carries an operationId, which names the method.
		"listUsers(",
		"createUser(",
		"body: CreateUserReq",
		"Promise<User[]>",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("client.ts missing %q", want)
		}
	}
}

func TestRunSDKGo(t *testing.T) {
	out := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"-dir", exampleDir, "-sdk", "go", "-sdk-package", "demo", "-sdk-out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run = %d: %s", code, stderr.String())
	}

	data, err := os.ReadFile(filepath.Join(out, "client.go"))
	if err != nil {
		t.Fatalf("client.go not written: %v", err)
	}
	src := string(data)
	for _, want := range []string{
		"package demo",
		"type User struct",
		"type CreateUserReq struct",
		"func (c *Client) ListUsers(",
		"func (c *Client) CreateUser(",
		"([]User, error)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("client.go missing %q", want)
		}
	}
}

// An unknown language fails rather than writing something unusable.
func TestRunSDKUnknownLanguage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-dir", exampleDir, "-sdk", "rust", "-sdk-out", t.TempDir()}, &stdout, &stderr)
	if code == 0 {
		t.Error("expected a non-zero exit for an unknown language")
	}
}

// Without -sdk-out the client lands in ./sdk, relative to the working
// directory the command ran in.
func TestRunSDKDefaultOutputDir(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// The scan directory is resolved before the chdir; ./sdk is relative to
	// where the command runs, which is the point of the test.
	scanDir, err := filepath.Abs(exampleDir)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })

	var stdout, stderr bytes.Buffer
	code := run([]string{"-dir", scanDir, "-sdk", "ts"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run = %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tmp, "sdk", "client.ts")); err != nil {
		t.Fatalf("client.ts not written to ./sdk: %v", err)
	}
}
