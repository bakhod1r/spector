package sdk

import "strings"

// words splits an identifier fragment into its component words, handling the
// three conventions a scanned schema can carry at once: snake_case and
// kebab-case (split on the separator), and camelCase/PascalCase (split on a
// lower-to-upper hump). "userID" → [user ID], "created_at" → [created at],
// "HTTPServer" → [HTTP Server].
func words(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		if !isLetter && !isDigit {
			flush()
			continue
		}
		if r >= 'A' && r <= 'Z' && i > 0 {
			prev := runes[i-1]
			// A hump starts a new word: lower→upper (userID) or the last upper of
			// an acronym before a new word (HTTPServer → HTTP, Server).
			prevLower := prev >= 'a' && prev <= 'z'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			if prevLower || (nextLower && prev >= 'A' && prev <= 'Z') {
				flush()
			}
		}
		cur.WriteRune(r)
	}
	flush()
	return out
}

// pascalCase joins the words with each capitalised: "created_at" → "CreatedAt".
func pascalCase(s string) string {
	var b strings.Builder
	for _, w := range words(s) {
		b.WriteString(strings.ToUpper(w[:1]) + strings.ToLower(w[1:]))
	}
	return b.String()
}

// camelCase is pascalCase with a lower-case first letter: "created_at" →
// "createdAt".
func camelCase(s string) string {
	p := pascalCase(s)
	if p == "" {
		return ""
	}
	return strings.ToLower(p[:1]) + p[1:]
}

// snakeCase joins the words in lower case with underscores: "createdAt" →
// "created_at".
func snakeCase(s string) string {
	parts := words(s)
	for i, w := range parts {
		parts[i] = strings.ToLower(w)
	}
	return strings.Join(parts, "_")
}
