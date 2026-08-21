// Package fieldkind generates a value from what a field is called.
//
// A schema can say a property is a string and nothing more. Everything that
// makes the value recognisable — that it is a token, an address, a timezone —
// lives in its name, and synth already knows how to read one: infer.Kind maps
// "email", "token", "timezone" onto real kinds. What it does not do is read a
// *qualified* name: its table has "token" and "id" but not "access_token" or
// "session_id", which is what real payloads are full of. Alias closes that gap
// by falling back to the head word, and String turns the result into a value.
package fieldkind

import (
	"fmt"
	"strings"
	"sync"

	"github.com/bakhod1r/synth"
	"github.com/bakhod1r/synth/infer"
)

// Alias returns the name synth recognises for this field, and whether it found
// one. It tries the name itself first, then progressively shorter tails
// ("access_token" -> "token"), then the head ("email_address" -> "email"), and
// finally the singular of a plural ("emails" -> "email").
func Alias(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	for _, cand := range candidates(name) {
		if _, byName := infer.Kind(cand, "string"); byName {
			return cand, true
		}
	}
	return "", false
}

// candidates lists the names to try, most specific first. The full name has to
// win: "phone_type" is its own kind, not a phone.
func candidates(name string) []string {
	words := splitWords(name)
	if len(words) == 0 {
		return nil
	}
	var out []string
	out = append(out, strings.Join(words, "_"))
	// Tails: access_token -> token; user_phone_number -> phone_number -> number.
	for i := 1; i < len(words); i++ {
		out = append(out, strings.Join(words[i:], "_"))
	}
	// Heads: email_address -> email. A qualified name usually names its head.
	for i := len(words) - 1; i > 0; i-- {
		out = append(out, strings.Join(words[:i], "_"))
	}
	// A collection is named for what it holds: emails -> email.
	var singular []string
	for _, w := range out {
		if s, ok := depluralize(w); ok {
			singular = append(singular, s)
		}
	}
	return append(out, singular...)
}

// splitWords breaks a field name into lowercase words on separators and on
// camelCase boundaries: "accessToken" and "access_token" split the same.
func splitWords(name string) []string {
	spaced := strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(name)
	var b strings.Builder
	prev := byte(0)
	for i := 0; i < len(spaced); i++ {
		c := spaced[i]
		if i > 0 && c >= 'A' && c <= 'Z' && (prev >= 'a' && prev <= 'z' || prev >= '0' && prev <= '9') {
			b.WriteByte(' ')
		}
		b.WriteByte(c)
		prev = c
	}
	return strings.Fields(strings.ToLower(b.String()))
}

// depluralize handles the plural forms a field name actually takes. It is
// deliberately narrow: a wrong guess here invents a kind the field is not.
func depluralize(w string) (string, bool) {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 3:
		return w[:len(w)-3] + "y", true
	case strings.HasSuffix(w, "ses") && len(w) > 3:
		return w[:len(w)-2], true
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 1:
		return w[:len(w)-1], true
	}
	return "", false
}

// String generates a value for the field, seeded by the caller: a mock wants
// the same body every run, a body generator wants a fresh one. ok is false when
// the name says nothing, and the caller should keep its own placeholder rather
// than invent prose for a field nothing is known about.
func String(name string, seed uint64) (string, bool) {
	alias, ok := Alias(name)
	if !ok {
		return "", false
	}
	spec, err := specFor(alias)
	if err != nil {
		return "", false
	}
	recs, err := spec.Generate(1, synth.WithSeed(seed))
	if err != nil || len(recs) == 0 {
		return "", false
	}
	// Not every kind generates a Go string — a uuid comes back as its own type
	// — but every one of them has a textual form, which is what a JSON string
	// field holds.
	v, ok := recs[0][alias]
	if !ok || v == nil {
		return "", false
	}
	s := fmt.Sprint(v)
	if s == "" {
		return "", false
	}
	return s, true
}

// specCache keeps one compiled generator per alias: compiling is the expensive
// part, and a document generates the same names over and over.
var (
	specMu    sync.Mutex
	specCache = map[string]*synth.SchemaFile{}
)

func specFor(alias string) (*synth.SchemaFile, error) {
	specMu.Lock()
	defer specMu.Unlock()
	if s, ok := specCache[alias]; ok {
		return s, nil
	}
	// A one-property JSON Schema is synth's public front door that reads names:
	// schemafe runs the same infer.Kind lookup on the property name.
	doc := fmt.Sprintf(`{"type":"object","properties":{%q:{"type":"string"}}}`, alias)
	s, err := synth.JSONSchemaBytes([]byte(doc))
	if err != nil {
		return nil, err
	}
	specCache[alias] = s
	return s, nil
}
