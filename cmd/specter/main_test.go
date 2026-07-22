package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	exec2 "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const ginSrc = `package app

import "github.com/gin-gonic/gin"

type Widget struct {
	ID int ` + "`json:\"id\"`" + `
}

func Register(r *gin.Engine) {
	r.GET("/widgets", func(c *gin.Context) {})
}
`

const protoSrc = `syntax = "proto3";
package demo.v1;
message Ping { string msg = 1; }
service Echo { rpc Say(Ping) returns (Ping); }
`

const sdlSrc = `type Thing { id: ID! }
type Query { thing(id: ID!): Thing }
`

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// exec runs the CLI and returns its exit code plus both streams.
func exec(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestOpenAPIToStdout(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, stdout, stderr := exec(t, "-dir", dir, "-title", "Widgets", "-version", "3.0")

	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	var doc struct {
		Info struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if doc.Info.Title != "Widgets" || doc.Info.Version != "3.0" {
		t.Errorf("info = %+v, want the flags applied", doc.Info)
	}
	if _, ok := doc.Paths["/widgets"]; !ok {
		t.Errorf("paths = %v, want /widgets", doc.Paths)
	}
}

// Output must end with a newline so it composes with shell pipelines.
func TestStdoutEndsWithNewline(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	_, stdout, _ := exec(t, "-dir", dir)
	if !strings.HasSuffix(stdout, "\n") {
		t.Error("output does not end with a newline")
	}
}

func TestWritesToFile(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	target := filepath.Join(t.TempDir(), "openapi.json")

	code, stdout, stderr := exec(t, "-dir", dir, "-o", target)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty when -o is set", stdout)
	}
	if !strings.Contains(stderr, "wrote") {
		t.Errorf("stderr = %q, want a confirmation", stderr)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Error("written file is not valid JSON")
	}
}

func TestGrpcMode(t *testing.T) {
	dir := writeTree(t, map[string]string{"api/echo.proto": protoSrc})
	code, stdout, stderr := exec(t, "-grpc", "-dir", dir, "-proto", filepath.Join(dir, "api"))

	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	var doc struct {
		Services []struct {
			Name string `json:"name"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if len(doc.Services) != 1 || doc.Services[0].Name != "Echo" {
		t.Errorf("services = %+v, want Echo", doc.Services)
	}
}

func TestGraphqlMode(t *testing.T) {
	dir := writeTree(t, map[string]string{"sdl/schema.graphql": sdlSrc})
	code, stdout, stderr := exec(t, "-graphql", "-dir", dir, "-graphqlDir", filepath.Join(dir, "sdl"))

	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	var doc struct {
		Queries []struct {
			Name string `json:"name"`
		} `json:"queries"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if len(doc.Queries) != 1 || doc.Queries[0].Name != "thing" {
		t.Errorf("queries = %+v, want thing", doc.Queries)
	}
}

// -grpc wins over -graphql when both are given, matching the branch order.
func TestGrpcTakesPrecedenceOverGraphql(t *testing.T) {
	dir := writeTree(t, map[string]string{"api/echo.proto": protoSrc})
	_, stdout, _ := exec(t, "-grpc", "-graphql", "-dir", dir, "-proto", filepath.Join(dir, "api"))
	if !strings.Contains(stdout, `"services"`) {
		t.Errorf("stdout does not look like a gRPC document:\n%s", stdout)
	}
}

// The proto/graphql dirs default to -dir when not given explicitly.
func TestScanDirsDefaultToDir(t *testing.T) {
	protoOnly := writeTree(t, map[string]string{"echo.proto": protoSrc})
	if code, stdout, stderr := exec(t, "-grpc", "-dir", protoOnly); code != 0 || !strings.Contains(stdout, "Echo") {
		t.Errorf("grpc: exit %d, stderr %s", code, stderr)
	}
	sdlOnly := writeTree(t, map[string]string{"schema.graphql": sdlSrc})
	if code, stdout, stderr := exec(t, "-graphql", "-dir", sdlOnly); code != 0 || !strings.Contains(stdout, "thing") {
		t.Errorf("graphql: exit %d, stderr %s", code, stderr)
	}
}

// ---- empty results ----

// Finding nothing is not a failure: the document is still emitted and the exit
// code stays 0, with a warning naming the directory that was searched.
func TestEmptyResultsWarnButSucceed(t *testing.T) {
	empty := writeTree(t, map[string]string{"readme.txt": "nothing here"})

	cases := []struct {
		name     string
		args     []string
		wantWarn string
	}{
		{"openapi", []string{"-dir", empty}, "no routes found"},
		{"grpc", []string{"-grpc", "-dir", empty}, "no gRPC services found"},
		{"graphql", []string{"-graphql", "-dir", empty}, "no GraphQL schema found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := exec(t, tc.args...)
			if code != 0 {
				t.Errorf("exit = %d, want 0", code)
			}
			if !strings.Contains(stderr, tc.wantWarn) {
				t.Errorf("stderr = %q, want %q", stderr, tc.wantWarn)
			}
			if !strings.Contains(stderr, empty) {
				t.Errorf("warning does not name the directory searched: %q", stderr)
			}
			if !json.Valid([]byte(stdout)) {
				t.Errorf("stdout is not valid JSON:\n%s", stdout)
			}
		})
	}
}

// When an explicit scan dir is given the warning names that dir, not -dir.
func TestEmptyWarningNamesExplicitScanDir(t *testing.T) {
	root := writeTree(t, map[string]string{"readme.txt": "x"})
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, stderr := exec(t, "-grpc", "-dir", root, "-proto", sub); !strings.Contains(stderr, sub) {
		t.Errorf("stderr = %q, want the -proto dir named", stderr)
	}
	if _, _, stderr := exec(t, "-graphql", "-dir", root, "-graphqlDir", sub); !strings.Contains(stderr, sub) {
		t.Errorf("stderr = %q, want the -graphqlDir named", stderr)
	}
}

// ---- failures ----

// A scan failure in the gRPC and GraphQL modes exits the same way as OpenAPI.
func TestGrpcAndGraphqlScanFailuresExit1(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	for _, mode := range []string{"-grpc", "-graphql"} {
		t.Run(mode, func(t *testing.T) {
			code, _, stderr := exec(t, mode, "-dir", missing)
			if code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if !strings.Contains(stderr, "specter:") {
				t.Errorf("stderr = %q, want a prefixed error", stderr)
			}
		})
	}
}

func TestScanFailureExits1(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	code, _, stderr := exec(t, "-dir", missing)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "specter:") {
		t.Errorf("stderr = %q, want a prefixed error", stderr)
	}
}

func TestUnwritableOutputExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	// A path whose parent is a file, not a directory.
	blocked := filepath.Join(dir, "main.go", "out.json")

	code, _, stderr := exec(t, "-dir", dir, "-o", blocked)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "specter:") {
		t.Errorf("stderr = %q, want a prefixed error", stderr)
	}
}

// A bad flag is a usage error, distinct from a scan failure.
func TestUnknownFlagExits2(t *testing.T) {
	code, _, stderr := exec(t, "-nonsense")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if stderr == "" {
		t.Error("no usage message on an unknown flag")
	}
}

// A stdout that refuses writes must be reported, not ignored.
func TestStdoutWriteFailureExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	var stderr bytes.Buffer
	if code := run([]string{"-dir", dir}, failingWriter{}, &stderr); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "specter:") {
		t.Errorf("stderr = %q, want a prefixed error", stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

// ---- main ----

// main calls os.Exit, so it can only be observed from a separate process. The
// test binary re-executes itself with a marker env var, which makes TestMain
// run main() instead of the suite.
const reexecEnv = "SPECTER_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(reexecEnv) == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func runMainInSubprocess(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec2.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), reexecEnv+"=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	code := 0
	if err != nil {
		var ee *exec2.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			t.Fatalf("running subprocess: %v", err)
		}
	}
	return code, stdout.String(), stderr.String()
}

func TestMainSucceeds(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, stdout, stderr := runMainInSubprocess(t, "-dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout is not valid JSON:\n%s", stdout)
	}
}

// The exit code has to propagate; a CI step depends on it.
func TestMainPropagatesFailureExitCode(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	code, _, stderr := runMainInSubprocess(t, "-dir", missing)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "specter:") {
		t.Errorf("stderr = %q, want a prefixed error", stderr)
	}
}

const configSrc = `{
  "title": "Shop",
  "version": "2.0",
  "servers": [{"url": "https://api.example.com", "description": "prod"}],
  "security": {
    "bearerAuth": {"type": "http", "scheme": "bearer", "bearerFormat": "JWT"}
  }
}`

// The document the CLI writes must be the document the console serves. Servers
// and security schemes are declared, not inferable, so without a config file
// the two disagree — and the file is the only place a map of schemes can
// reasonably be written.
func TestConfigFileFillsTheDocument(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc, "specter.json": configSrc})
	code, stdout, stderr := exec(t, "-dir", dir, "-config", filepath.Join(dir, "specter.json"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}

	var doc struct {
		Info struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
		Components struct {
			SecuritySchemes map[string]struct {
				Type   string `json:"type"`
				Scheme string `json:"scheme"`
			} `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(doc.Servers) != 1 || doc.Servers[0].URL != "https://api.example.com" {
		t.Errorf("servers = %+v, want the configured one", doc.Servers)
	}
	if s := doc.Components.SecuritySchemes["bearerAuth"]; s.Type != "http" || s.Scheme != "bearer" {
		t.Errorf("securitySchemes = %+v, want bearerAuth", doc.Components.SecuritySchemes)
	}
	if doc.Info.Title != "Shop" || doc.Info.Version != "2.0" {
		t.Errorf("info = %+v, want the file's title and version", doc.Info)
	}
}

// A file is a default, not an override: a flag the user actually typed wins.
// The version flag has a non-empty default, so "was it set" cannot be answered
// by looking at the value.
func TestFlagsBeatTheConfigFile(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc, "specter.json": configSrc})
	code, stdout, stderr := exec(t, "-dir", dir, "-config", filepath.Join(dir, "specter.json"), "-version", "9.9")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	var doc struct {
		Info struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Info.Version != "9.9" {
		t.Errorf("version = %q, want the flag to win", doc.Info.Version)
	}
	if doc.Info.Title != "Shop" {
		t.Errorf("title = %q, want the file's, which no flag overrode", doc.Info.Title)
	}
}

// A specter.json sitting in the scanned directory is picked up without being
// named, so the console and the CLI agree by default rather than by discipline.
func TestConfigFileFoundInScanDir(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc, "specter.json": configSrc})
	code, stdout, stderr := exec(t, "-dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "https://api.example.com") {
		t.Errorf("servers missing; the adjacent specter.json was not read:\n%s", stdout)
	}
}

// A config that cannot be parsed is a mistake to report, not to ignore: the
// document would silently lose everything the file was there to declare.
func TestBrokenConfigFileExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc, "bad.json": "{not json"})
	code, _, stderr := exec(t, "-dir", dir, "-config", filepath.Join(dir, "bad.json"))
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "bad.json") {
		t.Errorf("stderr = %q, want the file named", stderr)
	}
}

// An explicitly named file that is not there is a typo worth reporting; an
// absent specter.json is not.
func TestMissingNamedConfigExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, _, stderr := exec(t, "-dir", dir, "-config", filepath.Join(dir, "nope.json"))
	if code != 1 {
		t.Errorf("exit = %d, want 1, stderr: %s", code, stderr)
	}
}

// -contract writes artefacts that execute the document rather than restate it.
func TestContractGeneratesEveryArtefact(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "contract")

	code, _, stderr := exec(t, "-dir", dir, "-contract", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	for _, name := range []string{"requests.http", "contract_test.go", "check.go", "smoke.sh"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("%s was not written: %v", name, err)
		}
	}
	if !strings.Contains(stderr, "-tags contract") {
		t.Errorf("stderr = %q, want the run instructions", stderr)
	}
}

// smoke.sh is meant to be run, and a file without the execute bit is a papercut
// on the one artefact whose whole point is being cheap to run.
func TestSmokeScriptIsExecutable(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "contract")

	if code, _, stderr := exec(t, "-dir", dir, "-contract", out, "-contract-format", "curl"); code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	info, err := os.Stat(filepath.Join(out, "smoke.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("smoke.sh mode = %v, want the execute bit set", info.Mode().Perm())
	}
}

func TestContractFormatSelectsArtefacts(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "contract")

	if code, _, stderr := exec(t, "-dir", dir, "-contract", out, "-contract-format", "http"); code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "requests.http" {
		t.Errorf("wrote %v, want only requests.http", entries)
	}
}

func TestContractRejectsAnUnknownFormat(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	code, _, stderr := exec(t, "-dir", dir, "-contract", t.TempDir(), "-contract-format", "postman")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "postman") {
		t.Errorf("stderr = %q, want the bad format named", stderr)
	}
}

// The base URL decides where every generated artefact points, so a flag that
// was ignored would send a whole suite at the wrong host.
func TestContractBaseURLReachesTheArtefacts(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "contract")

	if code, _, stderr := exec(t, "-dir", dir, "-contract", out, "-contract-api", "https://staging.example.com"); code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(out, "requests.http"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "@baseUrl = https://staging.example.com") {
		t.Errorf("base URL was not applied:\n%s", data)
	}
}

// The generated Go must compile, and its package clause has to match wherever
// the user put it.
func TestContractPackageNameFollowsTheDirectory(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "api-checks")

	if code, _, stderr := exec(t, "-dir", dir, "-contract", out); code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(out, "check.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "package apichecks") {
		t.Errorf("package clause not derived from the directory:\n%s", firstLines(string(data), 12))
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
