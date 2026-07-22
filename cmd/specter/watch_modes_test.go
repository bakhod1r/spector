package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// waitFor polls cond until it holds, so a watch test never sleeps longer than
// it has to and never hangs the suite if the condition never arrives.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

// -all with -watch writes once and then keeps watching.
func TestRunAllWatch(t *testing.T) {
	fastWatch(t, 2)
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})
	out := t.TempDir()

	code, _, stderr := exec(t, "-dir", filepath.Join(dir, "app"), "-all", "-o", out, "-watch")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "openapi.json")); err != nil {
		t.Fatalf("document not written: %v", err)
	}
	if !strings.Contains(stderr, "watching") {
		t.Errorf("watch did not start: %q", stderr)
	}
}

// -sdk with -watch behaves the same way: generate, then keep going.
func TestRunSDKWatch(t *testing.T) {
	fastWatch(t, 2)
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})
	out := t.TempDir()

	code, _, stderr := exec(t, "-dir", filepath.Join(dir, "app"), "-sdk", "ts", "-sdk-out", out, "-watch")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "watching") {
		t.Errorf("watch did not start: %q", stderr)
	}
}

func TestSDKMkdirFailureExits1(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := exec(t, "-dir", dir, "-sdk", "ts", "-sdk-out", blocker); code != 1 {
		t.Errorf("exit = %d, want 1 (%s)", code, stderr)
	}
}

func TestSDKWriteFileFailureExits1(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	out := t.TempDir()
	if err := os.Chmod(out, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(out, 0o755) })
	if code, _, stderr := exec(t, "-dir", dir, "-sdk", "ts", "-sdk-out", out); code != 1 {
		t.Errorf("exit = %d, want 1 (%s)", code, stderr)
	}
}

// A regeneration that fails during -watch is reported and the watch continues;
// the first pass had already written a document.
func TestRunWatchRegenFailureIsReported(t *testing.T) {
	fastWatch(t, 40)
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})
	src := filepath.Join(dir, "app", "main.go")
	out := filepath.Join(t.TempDir(), "openapi.json")

	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"-dir", filepath.Join(dir, "app"), "-o", out, "-watch"}, &stdout, &stderr)
	}()

	// Unparsable source: the scan fails, so regen returns an error.
	waitFor(t, func() bool { return strings.Contains(stderr.String(), "watching") })
	os.WriteFile(src, []byte("package app\nfunc ("), 0o644)
	waitFor(t, func() bool { return strings.Contains(stderr.String(), "specter:") })

	<-done
}

// A write that fails during -watch is reported without ending the watch.
func TestRunWatchWriteFailureIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not block writes on windows")
	}
	fastWatch(t, 40)
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})
	src := filepath.Join(dir, "app", "main.go")
	outDir := t.TempDir()
	out := filepath.Join(outDir, "openapi.json")

	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"-dir", filepath.Join(dir, "app"), "-o", out, "-watch"}, &stdout, &stderr)
	}()

	waitFor(t, func() bool { return strings.Contains(stderr.String(), "watching") })
	// The document exists; making its directory read-only breaks the rewrite
	// but not the scan, so only the write can fail.
	os.Remove(out)
	if err := os.Chmod(outDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(outDir, 0o755) })
	os.WriteFile(src, []byte(ginSrc+"\n// changed\n"), 0o644)
	waitFor(t, func() bool { return strings.Contains(stderr.String(), "specter:") })

	<-done
}

// A successful regeneration during -watch rewrites the output and says so.
func TestRunWatchRewritesOnChange(t *testing.T) {
	fastWatch(t, 40)
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})
	src := filepath.Join(dir, "app", "main.go")
	out := filepath.Join(t.TempDir(), "openapi.json")

	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"-dir", filepath.Join(dir, "app"), "-o", out, "-watch"}, &stdout, &stderr)
	}()

	waitFor(t, func() bool { return strings.Contains(stderr.String(), "watching") })
	os.WriteFile(src, []byte(strings.Replace(ginSrc, "/widgets", "/gadgets", 1)), 0o644)
	waitFor(t, func() bool { return strings.Contains(stderr.String(), "change detected") })
	<-done

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/gadgets") {
		t.Errorf("the rewritten document does not reflect the change: %s", data)
	}
}

// A stdio the server cannot read from is an error the CLI reports, not a
// silent success.
func TestRunMCPStdioFailureExits1(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	r.Close() // reading a closed descriptor fails rather than reaching EOF
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-mcp"}, &stdout, &stderr); code != 1 {
		t.Errorf("exit = %d, want 1 (%s)", code, stderr.String())
	}
}

// A directory that is not there fingerprints as empty rather than panicking:
// WalkDir hands the error to the callback, which treats it as a change the
// next pass will see.
func TestFingerprintMissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	if got := fingerprint(missing); got == "" {
		t.Error("fingerprint of a missing directory returned nothing")
	}
}

// -mcp serves over stdio; with stdin already at EOF the server returns at once
// and the CLI exits cleanly.
func TestRunMCPServesAndExits(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-mcp"}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0 (%s)", code, stderr.String())
	}
}
