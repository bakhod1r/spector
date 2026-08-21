package mock

import (
	"hash/fnv"

	"github.com/bakhod1r/spector/internal/core"
	"github.com/bakhod1r/spector/internal/fieldkind"
)

// namedString generates a value from what the property is called.
//
// A schema-only mock can say a field is a string and nothing more, so every
// unformatted string came back as the literal "string": a body of
// {"access_token":"string","session_id":"string","user_id":"string"} tells a
// reader nothing about what those fields hold. The name is the signal the
// schema does not carry.
//
// The value is deterministic, seeded from the field's name, so the same
// document always mocks to the same body. Exports (Postman, HAR, markdown) and
// generated contract tests all render Sample output, and a body that changed
// per run would make every one of them produce a spurious diff.
//
// ok is false when the name says nothing, or when the generated value would
// break the schema it stands for — the caller then keeps the plain placeholder.
func namedString(name string, schema *core.Schema) (string, bool) {
	s, ok := fieldkind.String(name, seedFor(name))
	if !ok {
		return "", false
	}
	// The generated value still has to satisfy its own schema: a length bound
	// the document declares is a constraint on the mock too.
	if schema != nil && schema.MaxLength != nil && len(s) > *schema.MaxLength {
		return "", false
	}
	if schema != nil && schema.MinLength != nil && len(s) < *schema.MinLength {
		return "", false
	}
	return s, true
}

// seedFor derives a stable seed from the field name, so a given field mocks to
// the same value on every run while different fields differ.
func seedFor(name string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return h.Sum64()
}
