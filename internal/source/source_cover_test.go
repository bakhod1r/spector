package source

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Asking for a line far past the end of the file yields nothing to show.
func TestReadPastTheEndOfTheFile(t *testing.T) {
	if _, err := Read(tree(t), "main.go", 5000); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want ErrNotExist", err)
	}
}

// A single line longer than the scanner's enlarged cap ends the read with an
// error rather than a silently truncated snippet.
func TestReadFailsOnAnAbsurdlyLongLine(t *testing.T) {
	root := tree(t)
	write(t, filepath.Join(root, "long.go"), "package app\n// "+strings.Repeat("x", 2*1024*1024)+"\n")
	if _, err := Read(root, "long.go", 1); err == nil {
		t.Error("a 2MB line must surface the scanner's error")
	}
}

// A file that exists but cannot be opened surfaces the open error.
func TestReadUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission bits do not bind here")
	}
	root := tree(t)
	locked := filepath.Join(root, "locked.go")
	write(t, locked, "package app\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	if _, err := Read(root, "locked.go", 1); err == nil {
		t.Error("an unreadable file must be an error")
	}
}

// Abs fails only when the working directory is gone; simulate exactly that.
func TestReadWhenTheWorkingDirectoryIsGone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cannot remove the working directory on Windows")
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	doomed := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(doomed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(doomed); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(doomed); err != nil {
		t.Fatal(err)
	}
	if _, err := Read("app", "main.go", 1); err == nil {
		t.Error("Abs on a relative root with no working directory must fail")
	}
}

// The root itself counts as inside the root.
func TestWithinAcceptsTheRootItself(t *testing.T) {
	if !within("/srv/app", "/srv/app") {
		t.Error("within(root, root) = false, want true")
	}
}
