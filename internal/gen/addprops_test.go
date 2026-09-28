package gen

import (
	"testing"

	"github.com/bakhod1r/spector/internal/core"
)

// A schema used only as a map value (map[string]User) is reached through
// additionalProperties. It must be pulled into components, or the reference
// dangles and is pruned to a bare object.
func TestWalkFollowsAdditionalProperties(t *testing.T) {
	b := &builder{
		schemas: map[string]*core.Schema{"User": {Type: "object"}},
		used:    map[string]bool{},
	}
	b.walk(&core.Schema{Type: "object", AdditionalProperties: &core.Schema{Ref: refPrefix + "User"}})
	if !b.used["User"] {
		t.Fatal("User reached only through additionalProperties was not pulled in")
	}
}
