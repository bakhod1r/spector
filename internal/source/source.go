// Package source serves the handler code behind an operation.
//
// Specter reads the AST, so it knows the file and line every operation came
// from. Showing that code in the console is the payoff: the documentation and
// the implementation cannot drift, because one is read from the other.
//
// Serving files is also the one place in Specter that touches the filesystem on
// behalf of a request, so the rules here are deliberately narrow: only .go
// files, only inside the scanned directory, only a window of lines.
package source

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Window is how many lines of context are returned around the requested line.
const Window = 40

// Snippet is a slice of a file, with the line numbers needed to render it.
type Snippet struct {
	File  string   `json:"file"`
	Start int      `json:"start"` // 1-based line number of Lines[0]
	Line  int      `json:"line"`  // the line that was asked for
	Lines []string `json:"lines"`
}

var (
	// ErrOutsideRoot is returned for any path that does not resolve to a file
	// inside the scanned directory.
	ErrOutsideRoot = errors.New("source: path outside the scanned directory")
	// ErrNotGo is returned for anything that is not a .go file.
	ErrNotGo = errors.New("source: not a Go file")
)

// Read returns a window of lines around line n of the file named rel, which is
// interpreted relative to root.
//
// The path is resolved and then checked against the resolved root, rather than
// being filtered for "..": filtering is a blocklist, and symlinks defeat it.
// Comparing the resolved paths answers the actual question — is the file that
// would be opened inside the tree the operator pointed us at.
func Read(root, rel string, n int) (*Snippet, error) {
	if filepath.Ext(rel) != ".go" {
		return nil, ErrNotGo
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// EvalSymlinks so a symlink inside the tree cannot point out of it. It
	// fails on a path that does not exist, which is the same answer we want to
	// give anyway: nothing to show.
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}

	target := filepath.Join(absRoot, filepath.FromSlash(rel))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, err
	}
	if !within(absRoot, resolved) {
		return nil, ErrOutsideRoot
	}

	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	start := n - Window/2
	if start < 1 {
		start = 1
	}
	end := start + Window

	snip := &Snippet{File: filepath.ToSlash(rel), Start: start, Line: n}
	sc := bufio.NewScanner(f)
	// Generated files can carry lines longer than the scanner's default cap;
	// without this a long line ends the read early and truncates the snippet.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for i := 1; sc.Scan(); i++ {
		if i < start {
			continue
		}
		if i >= end {
			break
		}
		snip.Lines = append(snip.Lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(snip.Lines) == 0 {
		return nil, os.ErrNotExist
	}
	return snip, nil
}

// within reports whether path is root or sits under it. The separator check
// stops "/srv/app-secrets" from passing as a child of "/srv/app".
func within(root, path string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
