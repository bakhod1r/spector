package sdk

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// A summary comes from a document the user may not control (-openapi). It is
// written into comments, so it must not be able to end the comment and put
// code into the client.
func TestSummaryCannotInjectCode(t *testing.T) {
	const payload = "INJECTED_CODE()"
	evil := "x\n" + payload + "\r\n*/ " + payload + " /* " + payload + " </summary> ?> " + payload
	for _, lang := range []string{"go", "ts", "js", "python", "java", "kotlin", "csharp", "rust", "ruby", "php"} {
		t.Run(lang, func(t *testing.T) {
			d := doc()
			for _, item := range d.Paths {
				for _, op := range item {
					if op != nil {
						op.Summary = evil
					}
				}
			}
			files, err := Generate(d, Options{Lang: lang})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				for i, line := range strings.Split(string(f.Data), "\n") {
					if !strings.Contains(line, payload) {
						continue
					}
					if !inComment(lang, line) {
						t.Fatalf("%s:%d: payload escaped the comment: %q", f.Name, i+1, line)
					}
				}
				if lang == "go" && strings.HasSuffix(f.Name, ".go") {
					if _, err := parser.ParseFile(token.NewFileSet(), f.Name, f.Data, parser.AllErrors); err != nil {
						t.Fatalf("generated Go does not parse: %v", err)
					}
				}
				if strings.Contains(string(f.Data), " ") {
					t.Fatalf("%s keeps a U+2028 line terminator", f.Name)
				}
			}
		})
	}
}

// inComment reports whether every payload on the line sits after a comment
// opener that no closer ends before it.
func inComment(lang, line string) bool {
	t := strings.TrimSpace(line)
	switch lang {
	case "python":
		return strings.HasPrefix(t, `"`) // docstring line
	case "ruby":
		return strings.HasPrefix(t, "#")
	case "csharp":
		// One <summary> element: a second closer means the text ended it.
		return strings.HasPrefix(t, "///") && strings.Count(t, "</summary>") <= 1
	case "rust":
		return strings.HasPrefix(t, "///")
	case "php":
		return strings.HasPrefix(t, "//") && !strings.Contains(t, "?>")
	case "go":
		return strings.HasPrefix(t, "//")
	default: // ts, js, java, kotlin: /** ... */ blocks
		return (strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/**")) && !strings.Contains(strings.TrimSuffix(t, "*/"), "*/")
	}
}
