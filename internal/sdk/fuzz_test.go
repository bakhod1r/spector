package sdk

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// FuzzGenerate feeds arbitrary OpenAPI JSON (the -openapi input is untrusted)
// to every emitter: none may panic, and the Go client must always parse.
func FuzzGenerate(f *testing.F) {
	f.Add(`{"openapi":"3.0.0","paths":{"/u/{id}":{"get":{"summary":"x\n*/ y","parameters":[{"name":"id","in":"path"}],"responses":{"200":{}}}}}}`)
	f.Add(`{"paths":{"/a%b":{"post":{"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"n":{"type":"integer"}}}}}}}}}}`)
	f.Add(`{"components":{"schemas":{"A":{"allOf":[{"$ref":"#/components/schemas/A"}]}}}}`)
	f.Fuzz(func(t *testing.T, src string) {
		var doc core.Document
		if json.Unmarshal([]byte(src), &doc) != nil {
			return
		}
		for _, lang := range []string{"go", "ts", "js", "python", "java", "kotlin", "csharp", "rust", "ruby", "php"} {
			files, err := Generate(&doc, Options{Lang: lang})
			if err != nil {
				continue
			}
			if lang != "go" {
				continue
			}
			for _, fl := range files {
				if _, err := parser.ParseFile(token.NewFileSet(), fl.Name, fl.Data, 0); err != nil {
					t.Fatalf("generated Go does not parse: %v\n%s", err, fl.Data)
				}
			}
		}
	})
}
