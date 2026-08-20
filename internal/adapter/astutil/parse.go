package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skipDirs are directory names that never hold routes worth documenting, and
// that are expensive or actively misleading to walk into: vendored copies of
// other people's code, JavaScript modules, and the deliberately broken sources
// under testdata (which go/parser is expected to choke on).
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	"testdata":     true,
}

// ParseDir parses every non-test Go file in dir and in the directories below
// it, and returns the files flat.
//
// It replaces parser.ParseDir in the adapters for two reasons, both of which
// were bugs a real project hit on its first scan:
//
// Test files are excluded. A route registered in a test — the router built by
// an admission_test.go to exercise a middleware — is not part of the API, and
// documenting `GET /` because a test mounted a stub there is worse than
// documenting nothing.
//
// Subdirectories are included. parser.ParseDir reads a single directory, so
// pointing the CLI at a backend root found nothing at all and the caller had
// to know which package registered the routes. Nothing about a project's
// layout is knowable in advance, so the walk finds them instead.
//
// A file that does not parse is skipped rather than failing the scan: one
// unbuildable package somewhere in a large tree must not cost the whole
// document. The parse error is only returned when it left nothing to scan.
func ParseDir(fset *token.FileSet, dir string, mode parser.Mode) ([]*ast.File, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree is skipped; the rest of the walk stands.
			return nil
		}
		if d.IsDir() {
			if path == dir {
				return nil
			}
			name := d.Name()
			if skipDirs[name] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Sorted so that everything downstream that resolves a name to the first
	// declaration wins deterministically, whatever order the filesystem
	// reported.
	sort.Strings(paths)

	var files []*ast.File
	var firstErr error
	for _, path := range paths {
		file, perr := parser.ParseFile(fset, path, nil, mode)
		if perr != nil {
			if firstErr == nil {
				firstErr = perr
			}
			continue
		}
		files = append(files, file)
	}
	if len(files) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return files, nil
}
