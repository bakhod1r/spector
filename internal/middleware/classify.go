package middleware

import (
	"strings"

	"github.com/user/specter/internal/core"
)

// patterns map a substring of a middleware's name to what it does and, for
// authentication, which security scheme it implies.
//
// Naming is a convention rather than a guarantee, so everything matched here is
// reported as inferred. The alternative — saying nothing — is worse: an API
// where every route is protected and the document says none of them are is
// actively misleading, and that is the state Specter was in before this.
//
// Order matters: the more specific names come first, because "auth" appears
// inside "oauth" and "basicauth" too.
var patterns = []struct {
	match  string
	kind   string
	scheme string // security scheme name, for authentication only
}{
	{"bearer", KindAuth, "bearerAuth"},
	{"jwt", KindAuth, "bearerAuth"},
	{"oauth", KindAuth, "oauth2"},
	{"basicauth", KindAuth, "basicAuth"},
	{"apikey", KindAuth, "apiKeyAuth"},
	{"api_key", KindAuth, "apiKeyAuth"},
	{"session", KindAuth, "sessionAuth"},
	{"authenticate", KindAuth, "bearerAuth"},
	{"authorize", KindAuth, "bearerAuth"},
	{"requireauth", KindAuth, "bearerAuth"},
	{"authrequired", KindAuth, "bearerAuth"},
	{"basic", KindAuth, "basicAuth"},
	{"auth", KindAuth, "bearerAuth"},

	{"cors", KindCORS, ""},
	{"ratelimit", KindRateLimit, ""},
	{"rate_limit", KindRateLimit, ""},
	{"throttle", KindRateLimit, ""},
	{"limiter", KindRateLimit, ""},
	{"logger", KindLogging, ""},
	{"logging", KindLogging, ""},
	{"recovery", KindRecovery, ""},
	{"recover", KindRecovery, ""},
	{"gzip", KindCompress, ""},
	{"compress", KindCompress, ""},
	{"deflate", KindCompress, ""},
}

// Classify names what a middleware appears to do, from its name alone.
//
// A name it does not recognise still yields a reported middleware with no kind:
// knowing that RequestID runs is useful context even when it says nothing about
// the API's contract, and the body may say more than the name does.
func Classify(name string) core.Middleware {
	m := core.Middleware{Name: name}
	lower := strings.ToLower(name)
	// Package-qualified names like jwt.Auth carry the signal in either part.
	lower = strings.ReplaceAll(lower, "_", "")
	lower = strings.ReplaceAll(lower, ".", "")

	for _, p := range patterns {
		if strings.Contains(lower, strings.ReplaceAll(p.match, "_", "")) {
			m.Kind = p.kind
			m.Scheme = p.scheme
			return m
		}
	}
	return m
}

// Schemes returns the security scheme names a chain implies, in a stable order.
// A route with two authentication middlewares requires both, so they are
// reported together rather than as alternatives.
func (c Chain) Schemes() []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range c {
		if m.Scheme == "" || seen[m.Scheme] {
			continue
		}
		seen[m.Scheme] = true
		out = append(out, m.Scheme)
	}
	return out
}

// SchemeDefinition returns a reasonable OpenAPI definition for an inferred
// scheme name. The definitions are conventional — a middleware named JWTAuth
// says nothing about where the token goes — so a project that cares about the
// detail should declare Security in Config, which always wins.
func SchemeDefinition(name string) core.SecurityScheme {
	switch name {
	case "bearerAuth":
		return core.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT",
			Description: "Inferred from middleware; declare Security in Config to describe it exactly."}
	case "basicAuth":
		return core.SecurityScheme{Type: "http", Scheme: "basic",
			Description: "Inferred from middleware."}
	case "apiKeyAuth":
		return core.SecurityScheme{Type: "apiKey", Name: "X-API-Key", In: "header",
			Description: "Inferred from middleware; the header name is a guess."}
	case "sessionAuth":
		return core.SecurityScheme{Type: "apiKey", Name: "session", In: "cookie",
			Description: "Inferred from middleware; the cookie name is a guess."}
	case "oauth2":
		return core.SecurityScheme{Type: "http", Scheme: "bearer",
			Description: "Inferred from OAuth middleware; flows are not inferable and must be declared."}
	}
	return core.SecurityScheme{Type: "http", Scheme: "bearer", Description: "Inferred from middleware."}
}
