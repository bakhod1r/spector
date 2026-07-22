package source

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tree builds a scannable directory: a Go file with numbered lines, plus a
// secret next to it that must never be reachable.
func tree(t *testing.T) (root string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "app")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	b.WriteString("package app\n")
	for i := 2; i <= 100; i++ {
		b.WriteString("// line ")
		b.WriteString(strings.Repeat("x", 0))
		b.WriteString(itoa(i))
		b.WriteString("\n")
	}
	write(t, filepath.Join(root, "main.go"), b.String())
	write(t, filepath.Join(base, "secret.go"), "package secret\nconst Token = \"hunter2\"\n")
	write(t, filepath.Join(base, ".env"), "PASSWORD=hunter2\n")
	return root
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func TestReadCentersOnTheLine(t *testing.T) {
	root := tree(t)
	snip, err := Read(root, "main.go", 50)
	if err != nil {
		t.Fatal(err)
	}
	if snip.Line != 50 {
		t.Errorf("Line = %d, want 50", snip.Line)
	}
	if snip.Start != 50-Window/2 {
		t.Errorf("Start = %d, want %d", snip.Start, 50-Window/2)
	}
	if len(snip.Lines) != Window {
		t.Errorf("got %d lines, want %d", len(snip.Lines), Window)
	}
	// The requested line must be where the caller can find it.
	got := snip.Lines[snip.Line-snip.Start]
	if !strings.Contains(got, "line 50") {
		t.Errorf("line 50 renders as %q", got)
	}
}

// Near the top of a file the window cannot be centred; it must clamp rather
// than ask for line 0 or negative offsets.
func TestReadClampsAtTheTop(t *testing.T) {
	root := tree(t)
	snip, err := Read(root, "main.go", 3)
	if err != nil {
		t.Fatal(err)
	}
	if snip.Start != 1 {
		t.Errorf("Start = %d, want 1", snip.Start)
	}
	if !strings.Contains(snip.Lines[0], "package app") {
		t.Errorf("first line = %q, want the package clause", snip.Lines[0])
	}
}

// Past the end of the file the read stops rather than padding.
func TestReadNearTheEnd(t *testing.T) {
	root := tree(t)
	snip, err := Read(root, "main.go", 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(snip.Lines) >= Window {
		t.Errorf("got %d lines from a 100-line file starting at %d", len(snip.Lines), snip.Start)
	}
}

// ---- containment ----

// These are the cases that decide whether this endpoint is a file-disclosure
// bug. Each one names a real technique, not a hypothetical.
func TestReadRefusesEscapes(t *testing.T) {
	root := tree(t)
	cases := map[string]string{
		"parent traversal":     "../secret.go",
		"nested traversal":     "sub/../../secret.go",
		"absolute path":        filepath.Join(filepath.Dir(root), "secret.go"),
		"non-Go file":          "../.env",
		"non-Go extension":     "main.txt",
		"no extension":         "main",
		"empty":                "",
		"traversal to /etc":    "../../../../../../etc/passwd",
		"encoded-looking dots": "..%2fsecret.go",
	}
	for name, rel := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Read(root, rel, 1); err == nil {
				t.Errorf("Read(%q) succeeded; it must not escape the scan dir", rel)
			}
		})
	}
}

// A symlink is the case a "reject .." filter misses: the path contains no dots
// at all, and only resolving it reveals where it goes.
func TestReadRefusesSymlinkOutOfTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privilege on Windows")
	}
	root := tree(t)
	target := filepath.Join(filepath.Dir(root), "secret.go")
	link := filepath.Join(root, "innocent.go")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot symlink: %v", err)
	}

	snip, err := Read(root, "innocent.go", 1)
	if err == nil {
		t.Fatalf("symlink out of the tree was served: %+v", snip)
	}
	if !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("err = %v, want ErrOutsideRoot", err)
	}
}

// A sibling directory whose name merely starts with the root's must not pass
// as a child of it.
func TestReadRefusesPrefixSibling(t *testing.T) {
	root := tree(t)
	sibling := root + "-secrets"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(sibling, "keys.go"), "package s\n")

	if _, err := Read(root, filepath.Join("..", filepath.Base(sibling), "keys.go"), 1); err == nil {
		t.Error("a path in app-secrets was served for root app")
	}
}

func TestReadMissingFile(t *testing.T) {
	if _, err := Read(tree(t), "nope.go", 1); err == nil {
		t.Error("a missing file must be an error, not an empty snippet")
	}
}

func TestReadNonGoIsRejectedBeforeTouchingDisk(t *testing.T) {
	if _, err := Read(tree(t), "main.txt", 1); !errors.Is(err, ErrNotGo) {
		t.Errorf("err = %v, want ErrNotGo", err)
	}
}
