package middleware

import (
	"go/ast"
	"sort"
	"strconv"
	"strings"

	"github.com/user/specter/internal/core"
)

// analyze reads a middleware's body and reports what it demands of a caller.
//
// This is the part that works on middleware nobody has heard of. Name matching
// can recognise a function called JWTAuth, but a real codebase is full of
// SignMiddleware, TenantGuard, PlatformCheck — names that mean everything to
// the team and nothing to a pattern list. Their bodies, though, are explicit:
// the headers they read are headers every guarded request must send, and the
// statuses they abort with are statuses every guarded endpoint can return.
//
// That is strictly better evidence than the name, because it is what the code
// does rather than what it was called.
func analyze(fd *ast.FuncDecl) (headers []string, statuses []int) {
	if fd == nil || fd.Body == nil {
		return nil, nil
	}

	seenHeader := map[string]bool{}
	seenStatus := map[int]bool{}

	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if name, ok := headerRead(sel, call); ok && !seenHeader[name] {
			seenHeader[name] = true
			headers = append(headers, name)
		}
		if code, ok := abortStatus(sel, call); ok && !seenStatus[code] {
			seenStatus[code] = true
			statuses = append(statuses, code)
		}
		return true
	})

	sort.Strings(headers)
	sort.Ints(statuses)
	return headers, statuses
}

// headerRead recognises a middleware reading a request header, in the gin and
// net/http spellings.
//
//	c.GetHeader("X-Sign")
//	c.Request.Header.Get("X-Sign")
//	r.Header.Get("X-Sign")
func headerRead(sel *ast.SelectorExpr, call *ast.CallExpr) (string, bool) {
	switch sel.Sel.Name {
	case "GetHeader":
		if len(call.Args) == 1 {
			return stringLit(call.Args[0])
		}
	case "Get":
		// Only a Get on something ending in .Header, so a map lookup or a
		// redis Get is not mistaken for a header read.
		if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "Header" && len(call.Args) == 1 {
			return stringLit(call.Args[0])
		}
	}
	return "", false
}

// abortStatus recognises a middleware rejecting a request. Only aborts count:
// a middleware that writes a status and continues is not refusing the request,
// and the handler's own responses are documented separately.
func abortStatus(sel *ast.SelectorExpr, call *ast.CallExpr) (int, bool) {
	switch sel.Sel.Name {
	case "AbortWithStatusJSON", "AbortWithStatus", "AbortWithError":
		if len(call.Args) >= 1 {
			return intLit(call.Args[0])
		}
	}
	return 0, false
}

func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	if s == "" {
		return "", false
	}
	return s, true
}

// intLit resolves a status code written as a literal or as an http constant.
func intLit(expr ast.Expr) (int, bool) {
	switch t := expr.(type) {
	case *ast.BasicLit:
		n, err := strconv.Atoi(t.Value)
		if err != nil {
			return 0, false
		}
		return n, true
	case *ast.SelectorExpr:
		// http.StatusUnauthorized and friends.
		if pkg, ok := t.X.(*ast.Ident); ok && pkg.Name == "http" {
			if n, ok := httpStatusConstants[t.Sel.Name]; ok {
				return n, true
			}
		}
	}
	return 0, false
}

// httpStatusConstants covers the codes a middleware plausibly aborts with.
// Resolving the whole net/http package would need a type checker; these are the
// ones that actually appear in a guard.
var httpStatusConstants = map[string]int{
	"StatusBadRequest":          400,
	"StatusUnauthorized":        401,
	"StatusPaymentRequired":     402,
	"StatusForbidden":           403,
	"StatusNotFound":            404,
	"StatusMethodNotAllowed":    405,
	"StatusConflict":            409,
	"StatusGone":                410,
	"StatusPreconditionFailed":  412,
	"StatusUnprocessableEntity": 422,
	"StatusTooManyRequests":     429,
	"StatusInternalServerError": 500,
	"StatusNotImplemented":      501,
	"StatusBadGateway":          502,
	"StatusServiceUnavailable":  503,
}

// authFromHeaders infers a security scheme from the headers a middleware
// requires, for middleware whose name says nothing.
//
// A middleware that reads Authorization is doing authentication whatever it is
// called; one that reads a custom signing header is an API-key scheme in
// everything but name.
func authFromHeaders(headers []string) (scheme string, def core.SecurityScheme, ok bool) {
	for _, h := range headers {
		lower := strings.ToLower(h)
		if lower == "authorization" {
			return "bearerAuth", core.SecurityScheme{
				Type: "http", Scheme: "bearer",
				Description: "Inferred from a middleware that reads the Authorization header.",
			}, true
		}
	}
	// A signing or key header is the other common shape: not a bearer token,
	// but unmistakably a credential.
	for _, h := range headers {
		lower := strings.ToLower(h)
		if strings.Contains(lower, "sign") || strings.Contains(lower, "key") ||
			strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
			return "signature", core.SecurityScheme{
				Type: "apiKey", Name: h, In: "header",
				Description: "Inferred from a middleware that requires this header. " +
					"Other headers it reads are documented as required parameters.",
			}, true
		}
	}
	return "", core.SecurityScheme{}, false
}
