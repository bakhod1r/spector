package core

import "testing"

func TestTypedExample(t *testing.T) {
	cases := []struct {
		typ, raw string
		want     any
	}{
		{"integer", "25", 25},
		{"integer", "abc", "abc"},
		{"number", "1.5", 1.5},
		{"number", "abc", "abc"},
		{"boolean", "True", true},
		{"boolean", "FALSE", false},
		{"boolean", "yes", "yes"},
		{"string", "hi", "hi"},
	}
	for _, c := range cases {
		if got := typedExample(c.typ, c.raw); got != c.want {
			t.Errorf("typedExample(%q, %q) = %v (%T), want %v", c.typ, c.raw, got, got, c.want)
		}
	}
}
