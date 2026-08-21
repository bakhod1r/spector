package fieldkind

import "testing"

// A qualified name is what real payloads are full of, and synth's own table
// has only the head word. Falling back to it is the whole point of Alias.
func TestAliasFindsTheHeadWord(t *testing.T) {
	for _, tc := range []struct {
		name, want string
	}{
		{"access_token", "token"},
		{"refresh_token", "token"},
		{"session_id", "id"},
		{"user_id", "id"},
		{"accessToken", "token"},
		{"email_address", "email_address"}, // synth knows this one outright
		{"emails", "email"},
		{"timezone", "timezone"},
	} {
		got, ok := Alias(tc.name)
		if !ok || got != tc.want {
			t.Errorf("Alias(%q) = %q, %v; want %q, true", tc.name, got, ok, tc.want)
		}
	}
}

// A name nothing is known about must say so, rather than have a kind invented
// for it.
func TestAliasRefusesUnknownNames(t *testing.T) {
	for _, name := range []string{"", "a", "xyzzy", "frobnicate"} {
		if got, ok := Alias(name); ok {
			t.Errorf("Alias(%q) = %q, true; want no match", name, got)
		}
	}
}

// The same seed gives the same value: a mock body has to be stable across runs.
func TestStringIsSeeded(t *testing.T) {
	a, ok := String("access_token", 42)
	if !ok || a == "" {
		t.Fatalf("String(access_token) = %q, %v", a, ok)
	}
	if b, _ := String("access_token", 42); b != a {
		t.Errorf("same seed gave %q then %q", a, b)
	}
	if c, _ := String("access_token", 43); c == a {
		t.Errorf("a different seed gave the same value %q", a)
	}
}

// Identifier fields are the commonest qualified name of all.
func TestStringForIdentifiers(t *testing.T) {
	for _, name := range []string{"session_id", "user_id", "device_id"} {
		got, ok := String(name, 7)
		t.Logf("%s -> %q (%v)", name, got, ok)
		if !ok || got == "" {
			t.Errorf("String(%q) = %q, %v; want a value", name, got, ok)
		}
	}
}
