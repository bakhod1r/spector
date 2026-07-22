package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fastWatch shortens the poll interval and bounds the loop, so a watch test
// costs milliseconds rather than seconds and always terminates.
func fastWatch(t *testing.T, iterations int) {
	t.Helper()
	oldInterval, oldMax := watchInterval, watchMaxIterations
	watchInterval, watchMaxIterations = 5*time.Millisecond, iterations
	t.Cleanup(func() { watchInterval, watchMaxIterations = oldInterval, oldMax })
}

func TestFingerprintChangesWithContent(t *testing.T) {
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})
	before := fingerprint(dir)

	// Size differs, so the fingerprint must too — no reliance on mtime
	// resolution, which is coarse on some filesystems.
	if err := os.WriteFile(filepath.Join(dir, "app/main.go"), []byte(ginSrc+"\n// touched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(dir); after == before {
		t.Error("fingerprint did not change after an edit")
	}
}

// Only source files count: writing an unrelated file must not retrigger.
func TestFingerprintIgnoresUnrelatedFiles(t *testing.T) {
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})
	before := fingerprint(dir)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(dir); after != before {
		t.Error("a .md file changed the fingerprint")
	}
}

func TestFingerprintSkipsVendorAndGit(t *testing.T) {
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})
	before := fingerprint(dir)
	if err := os.MkdirAll(filepath.Join(dir, "node_modules/pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules/pkg/index.go"), []byte("package pkg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(dir); after != before {
		t.Error("node_modules changed the fingerprint")
	}
}

// The loop regenerates on a change and leaves the output alone otherwise.
func TestWatchLoopRegeneratesOnChange(t *testing.T) {
	fastWatch(t, 4)
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})

	calls := 0
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- watchLoop(dir, &stderr, func() int { calls++; return 0 }) }()

	// One edit inside the loop's lifetime; the poll after it must fire emit.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "app/main.go"), []byte(ginSrc+"\n// v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("watchLoop = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchLoop did not return")
	}

	if calls == 0 {
		t.Error("emit was never called after a change")
	}
	if !strings.Contains(stderr.String(), "watching") {
		t.Errorf("no watch banner: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "change detected") {
		t.Errorf("change was not reported: %q", stderr.String())
	}
}

// An unchanged tree must not regenerate: a rebuild loop that fires on its own
// output would never settle.
func TestWatchLoopQuietWithoutChanges(t *testing.T) {
	fastWatch(t, 3)
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})

	calls := 0
	var stderr bytes.Buffer
	watchLoop(dir, &stderr, func() int { calls++; return 0 })
	if calls != 0 {
		t.Errorf("emit called %d times on an unchanged tree", calls)
	}
}

// A failing regeneration keeps the watch alive: the next save may fix it.
func TestWatchLoopSurvivesEmitFailure(t *testing.T) {
	fastWatch(t, 6)
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})

	calls := 0
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- watchLoop(dir, &stderr, func() int { calls++; return 1 }) }()

	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "app/main.go"), []byte(ginSrc+"\n// v2\n"), 0o644)
	time.Sleep(15 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "app/main.go"), []byte(ginSrc+"\n// v3 longer\n"), 0o644)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchLoop did not return")
	}
	if calls < 2 {
		t.Errorf("emit called %d times; a failure ended the watch", calls)
	}
}

// -watch with -o writes once and then keeps going.
func TestRunWatchWritesDocument(t *testing.T) {
	fastWatch(t, 2)
	dir := writeTree(t, map[string]string{"app/main.go": ginSrc})
	out := filepath.Join(t.TempDir(), "openapi.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-dir", filepath.Join(dir, "app"), "-o", out, "-watch"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run = %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("document not written: %v", err)
	}
	if !strings.Contains(stderr.String(), "watching") {
		t.Errorf("watch did not start: %q", stderr.String())
	}
}
