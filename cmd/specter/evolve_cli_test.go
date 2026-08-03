package main

import (
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo builds a real repository with two commits, so the -since path is
// exercised the way it runs in practice: a revision exported with git archive
// and scanned, not a fixture standing in for one.
func gitRepo(t *testing.T, first, second string) string {
	t.Helper()
	if _, err := osexec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()

	run := func(args ...string) {
		cmd := osexec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	run("git", "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", ".")
	run("git", "commit", "-q", "-m", "first")

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", ".")
	// --allow-empty so a test that wants two commits of identical content (it
	// supplies the difference another way) still gets a HEAD~1 to compare to.
	run("git", "commit", "-q", "--allow-empty", "-m", "second")
	return dir
}

const twoRoutes = `package app

import "github.com/gin-gonic/gin"

func Register(r *gin.Engine) {
	r.GET("/widgets", func(c *gin.Context) {})
	r.GET("/gadgets", func(c *gin.Context) {})
}
`

const oneRoute = `package app

import "github.com/gin-gonic/gin"

func Register(r *gin.Engine) {
	r.GET("/widgets", func(c *gin.Context) {})
}
`

// Removing an endpoint between the previous commit and the working tree is a
// breaking change, and -since HEAD~1 has to see it — which means git archive
// really exported and scanned the old revision.
func TestEvolveSinceDetectsARemovedEndpoint(t *testing.T) {
	dir := gitRepo(t, twoRoutes, oneRoute)

	code, stdout, stderr := exec3(t, dir, "-dir", ".", "-evolve", "-since", "HEAD~1")
	if code != 0 {
		t.Fatalf("exit = %d (no -fail-on-breaking), stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "BREAKING") || !strings.Contains(stdout, "/gadgets") {
		t.Errorf("the removed endpoint was not reported:\n%s", stdout)
	}
}

// The working tree adds an endpoint the previous commit lacked; the direction
// matters, so this must read as an addition, not a removal.
func TestEvolveSinceSeesAnAddition(t *testing.T) {
	dir := gitRepo(t, oneRoute, twoRoutes)

	code, stdout, _ := exec3(t, dir, "-dir", ".", "-evolve", "-since", "HEAD~1")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "ADDITION") || !strings.Contains(stdout, "/gadgets") {
		t.Errorf("the added endpoint was not reported as an addition:\n%s", stdout)
	}
}

// -fail-on-breaking is what a CI job gates on.
func TestEvolveFailOnBreakingExitsNonZero(t *testing.T) {
	dir := gitRepo(t, twoRoutes, oneRoute)
	code, _, stderr := exec3(t, dir, "-dir", ".", "-evolve", "-since", "HEAD~1", "-fail-on-breaking")
	if code != 1 {
		t.Errorf("exit = %d, want 1 for a breaking change under -fail-on-breaking\n%s", code, stderr)
	}
}

// A baseline JSON is the git-free path, for two artefacts a pipeline already
// produced.
func TestEvolveAgainstABaselineDocument(t *testing.T) {
	repo := gitRepo(t, twoRoutes, twoRoutes) // content irrelevant; we scan the tree
	baseline := filepath.Join(t.TempDir(), "v1.json")
	// Produce a baseline from the two-route tree, then compare a one-route tree
	// against it.
	if code, out, _ := exec3(t, repo, "-dir", ".", "-o", baseline); code != 0 {
		t.Fatalf("baseline generation failed: %s", out)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte(oneRoute), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := exec3(t, repo, "-dir", ".", "-evolve", "-baseline", baseline, "-evolve-format", "json")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var rep struct {
		Summary struct{ Breaking int } `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout)
	}
	if rep.Summary.Breaking != 1 {
		t.Errorf("breaking = %d, want 1 (the removed endpoint)", rep.Summary.Breaking)
	}
}

// Exactly one baseline may be given; none, or several, is a usage error caught
// before anything runs.
func TestEvolveRejectsAmbiguousBaseline(t *testing.T) {
	dir := writeTree(t, map[string]string{"main.go": ginSrc})
	if code, _, _ := exec(t, "-dir", dir, "-evolve"); code != 1 {
		t.Error("no baseline should be an error")
	}
	if code, _, stderr := exec(t, "-dir", dir, "-evolve", "-since", "HEAD", "-baseline", "x.json"); code != 1 {
		t.Errorf("two baselines should be an error; stderr: %s", stderr)
	}
}

func TestEvolveRejectsAnUnknownFormat(t *testing.T) {
	dir := gitRepo(t, oneRoute, oneRoute)
	code, _, stderr := exec3(t, dir, "-dir", ".", "-evolve", "-since", "HEAD~1", "-evolve-format", "yaml")
	if code != 1 || !strings.Contains(stderr, "yaml") {
		t.Errorf("exit = %d, stderr = %q, want the bad format rejected", code, stderr)
	}
}

// exec3 runs the CLI with the working directory set, which the -since path
// needs: git archive reads the repository at the process's cwd.
func exec3(t *testing.T, workdir string, args ...string) (int, string, string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)
	return exec(t, args...)
}
