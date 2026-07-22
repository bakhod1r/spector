// Package adapter_test checks the one thing every adapter must agree on but
// none of them shares code for: that each route carries the position of the
// code it came from. Reading the AST is what lets Specter know this at all, and
// an adapter that forgets to record it loses the information silently — the
// document still validates, the console still renders, and only the "view
// source" link is quietly missing.
package adapter_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chiadapter "github.com/user/specter/internal/adapter/chi"
	echoadapter "github.com/user/specter/internal/adapter/echo"
	ginadapter "github.com/user/specter/internal/adapter/gin"
	stdlibadapter "github.com/user/specter/internal/adapter/stdlib"
	"github.com/user/specter/internal/core"
)

func adapters() map[string]core.Adapter {
	return map[string]core.Adapter{
		"gin":    &ginadapter.Adapter{},
		"chi":    &chiadapter.Adapter{},
		"echo":   &echoadapter.Adapter{},
		"stdlib": &stdlibadapter.Adapter{},
	}
}

func scan(t *testing.T, name string, a core.Adapter) (string, []core.Route) {
	t.Helper()
	dir := filepath.Join(name, "testdata", "sample")
	routes, _, err := a.Scan(dir)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(routes) == 0 {
		t.Fatalf("%s: no routes scanned from %s", name, dir)
	}
	return dir, routes
}

func TestEveryAdapterRecordsSource(t *testing.T) {
	for name, a := range adapters() {
		t.Run(name, func(t *testing.T) {
			_, routes := scan(t, name, a)
			for _, r := range routes {
				if r.Source == nil {
					t.Errorf("%s %s has no Source", r.Method, r.Path)
					continue
				}
				if r.Source.File == "" || r.Source.Line <= 0 {
					t.Errorf("%s %s: Source = %+v, want a file and a positive line",
						r.Method, r.Path, *r.Source)
				}
				if filepath.IsAbs(r.Source.File) {
					t.Errorf("%s %s: File is absolute (%q); the document must be portable",
						r.Method, r.Path, r.Source.File)
				}
			}
		})
	}
}

// The recorded line must actually contain the handler. A position that is
// merely well-formed but points at the wrong line is worse than none: the
// console would confidently open the wrong code.
func TestRecordedLineContainsTheHandler(t *testing.T) {
	for name, a := range adapters() {
		t.Run(name, func(t *testing.T) {
			dir, routes := scan(t, name, a)
			for _, r := range routes {
				if r.Source == nil || r.HandlerName == "" {
					continue // the fallback-to-call-site case is checked in astutil
				}
				line := readLine(t, filepath.Join(dir, r.Source.File), r.Source.Line)
				if !strings.Contains(line, r.HandlerName) {
					t.Errorf("%s %s -> %s:%d\n  line: %s\n  want it to mention %q",
						r.Method, r.Path, r.Source.File, r.Source.Line, line, r.HandlerName)
				}
			}
		})
	}
}

func readLine(t *testing.T, path string, n int) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("the recorded path does not resolve from the scan dir: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for i := 1; sc.Scan(); i++ {
		if i == n {
			return sc.Text()
		}
	}
	t.Fatalf("%s has fewer than %d lines", path, n)
	return ""
}

// Doc comments are where summaries, descriptions and the specter: directives
// live. An adapter that parses without ParseComments loses all three silently:
// the document still generates, and every summary is simply absent.
func TestEveryAdapterParsesDocComments(t *testing.T) {
	for name, a := range adapters() {
		t.Run(name, func(t *testing.T) {
			_, routes := scan(t, name, a)
			for _, r := range routes {
				if r.Summary != "" {
					return // at least one doc comment reached the routes
				}
			}
			t.Errorf("%s produced %d routes and not one summary; is it parsing without parser.ParseComments?",
				name, len(routes))
		})
	}
}

// Directives are the one thing a project writes by hand, so they have to work
// the same everywhere. gin, chi and stdlib all parsed without comments until
// this was noticed, which meant a directive was accepted and silently dropped —
// the worst outcome, since the author has no way to tell.
func TestEveryAdapterReadsDirectives(t *testing.T) {
	for name, a := range adapters() {
		t.Run(name, func(t *testing.T) {
			_, routes := scan(t, name, a)
			for _, r := range routes {
				if r.HandlerName == "listUsers" {
					if len(r.Tags) == 0 || r.Tags[0] != "users" {
						t.Errorf("tags = %v, want [users] from the specter:tags directive", r.Tags)
					}
					return
				}
			}
			t.Skip("fixture has no listUsers route")
		})
	}
}

// A directive is an instruction to the generator, not prose. Leaving it in the
// description would print "specter:tags users" in the rendered documentation.
func TestDirectivesAreStrippedFromTheDescription(t *testing.T) {
	for name, a := range adapters() {
		t.Run(name, func(t *testing.T) {
			_, routes := scan(t, name, a)
			for _, r := range routes {
				if strings.Contains(r.Summary, "specter:") || strings.Contains(r.Description, "specter:") {
					t.Errorf("%s %s leaks a directive into its text: %q / %q",
						r.Method, r.Path, r.Summary, r.Description)
				}
			}
		})
	}
}
