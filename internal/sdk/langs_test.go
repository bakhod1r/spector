package sdk

import (
	"strings"
	"testing"
)

// allLangs is every language the generator claims to support. The table tests
// below run the same structural checks against each, so a new language is one
// row plus its emitter.
var allLangs = []struct {
	lang string
	file string
}{
	{"go", "client.go"},
	{"ts", "client.ts"},
	{"python", "client.py"},
	{"js", "client.js"},
	{"ruby", "client.rb"},
	{"php", "client.php"},
	{"csharp", "Client.cs"},
	{"rust", "lib.rs"},
	{"kotlin", "Client.kt"},
	{"java", "Client.java"},
}

// TestEveryLanguageGenerates is the smoke test: every advertised language
// produces at least one non-empty file without error, for a document that
// exercises schemas, composition, path params, a query, and a request body.
func TestEveryLanguageGenerates(t *testing.T) {
	doc := composedDoc()
	for _, tc := range allLangs {
		t.Run(tc.lang, func(t *testing.T) {
			files, err := Generate(doc, Options{Lang: tc.lang, Package: "c"})
			if err != nil {
				t.Fatalf("Generate(%s): %v", tc.lang, err)
			}
			if len(files) == 0 {
				t.Fatalf("Generate(%s): no files", tc.lang)
			}
			var total int
			var named bool
			for _, f := range files {
				total += len(f.Data)
				if f.Name == tc.file {
					named = true
				}
			}
			if total == 0 {
				t.Fatalf("Generate(%s): all files empty", tc.lang)
			}
			if !named {
				t.Errorf("Generate(%s): expected a %s among the output", tc.lang, tc.file)
			}
		})
	}
}

// TestComposedFieldReachesEveryLanguage guards the whole point of allOf
// support: the embedded type's field (createdAt, from Audit) must appear in the
// composing type's output in every language, however that language spells
// composition. Dropping allOf would silently lose it.
func TestComposedFieldReachesEveryLanguage(t *testing.T) {
	doc := composedDoc()
	for _, tc := range allLangs {
		t.Run(tc.lang, func(t *testing.T) {
			files, err := Generate(doc, Options{Lang: tc.lang, Package: "c"})
			if err != nil {
				t.Fatalf("Generate(%s): %v", tc.lang, err)
			}
			var src strings.Builder
			for _, f := range files {
				src.Write(f.Data)
				src.WriteByte('\n')
			}
			// The wire key is createdAt; different emitters may present it as
			// created_at (snake) or createdAt (as-is), so accept either.
			s := src.String()
			if !strings.Contains(s, "createdAt") && !strings.Contains(s, "created_at") {
				t.Errorf("Generate(%s): composed field createdAt missing from output", tc.lang)
			}
		})
	}
}

// TestOperationReachesEveryLanguage checks the operation surface: the method
// name derived for GET /users/{id} must appear, in whatever casing the language
// uses.
func TestOperationReachesEveryLanguage(t *testing.T) {
	doc := composedDoc()
	for _, tc := range allLangs {
		t.Run(tc.lang, func(t *testing.T) {
			files, err := Generate(doc, Options{Lang: tc.lang, Package: "c"})
			if err != nil {
				t.Fatalf("Generate(%s): %v", tc.lang, err)
			}
			var src strings.Builder
			for _, f := range files {
				src.Write(f.Data)
			}
			s := strings.ToLower(src.String())
			// deriveName gives getUsersById; accept snake/camel/pascal variants.
			if !strings.Contains(s, "getusersbyid") && !strings.Contains(s, "get_users_by_id") {
				t.Errorf("Generate(%s): operation method missing from output", tc.lang)
			}
		})
	}
}
