package admin

import (
	"strings"
	"testing"
)

func TestQuoteSlice(t *testing.T) {
	if got := quoteSlice([]string{"a", "b"}); got != `[]string{"a", "b"}` {
		t.Errorf("got %s", got)
	}
	if got := quoteSlice(nil); got != "[]string{}" {
		t.Errorf("got %s", got)
	}
}

func TestWriteFields(t *testing.T) {
	var b strings.Builder
	writeFields(&b, "Fields", nil)
	if b.Len() != 0 {
		t.Errorf("empty fields wrote %q", b.String())
	}

	writeFields(&b, "Fields", []Field{{
		Name:     "avatar",
		Label:    "Avatar",
		Type:     "string",
		Format:   "uri",
		Required: true,
		Primary:  true,
		Ref:      "User",
		Image:    true,
		Enum:     []string{"a", "b"},
	}})
	out := b.String()
	for _, want := range []string{
		`Name: "avatar"`, `Label: "Avatar"`, `Type: "string"`, `Format: "uri"`,
		"Required: true", "Primary: true", `Ref: "User"`, "Image: true",
		`Enum: []string{"a", "b"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %s:\n%s", want, out)
		}
	}
}
