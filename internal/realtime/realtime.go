// Package realtime recognises handlers that do not answer with a JSON body at
// all: they upgrade the connection to a WebSocket, or hold it open and stream
// server-sent events.
//
// These endpoints are the ones documentation usually loses. They appear in the
// routing table like any other route, so a generator that only looks at
// registrations lists them as ordinary GETs returning nothing — which is worse
// than omitting them, because it describes them wrongly. Reading the handler
// body is what tells them apart.
//
// Detection is syntactic, like the rest of Specter, but the signals here are
// far more specific than a method name: upgrading a connection and writing a
// text/event-stream content type are things code does for exactly one reason.
package realtime

import (
	"go/ast"
	"strings"

	"github.com/user/specter/internal/core"
)

// Kinds of realtime endpoint.
const (
	WebSocket = "websocket"
	SSE       = "sse"
)

// maxDepth bounds how far a handler is followed into helpers. A handler that
// delegates the whole upgrade to a shared function is common enough to be worth
// following, but the signals are local by nature, so this stays shallow.
const maxDepth = 2

// Detect reports whether a handler serves a realtime protocol, and which.
// It returns "" for ordinary handlers, which is almost all of them.
func Detect(fd *ast.FuncDecl, funcs map[string]*ast.FuncDecl) string {
	return detect(fd, funcs, map[string]bool{}, 0)
}

func detect(fd *ast.FuncDecl, funcs map[string]*ast.FuncDecl, seen map[string]bool, depth int) string {
	if fd == nil || fd.Body == nil || depth > maxDepth || seen[fd.Name.Name] {
		return ""
	}
	seen[fd.Name.Name] = true

	found := ""
	var follow []*ast.FuncDecl

	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if found == WebSocket {
			return false // the strongest signal; nothing will override it
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch {
		case isUpgrade(call):
			found = WebSocket
			return false
		case isSSE(call):
			// Not returned immediately: a handler may set the SSE content type
			// and then upgrade, and WebSocket is the more specific answer.
			found = SSE
		default:
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if f := funcs[sel.Sel.Name]; f != nil {
					follow = append(follow, f)
				}
			}
			if id, ok := call.Fun.(*ast.Ident); ok {
				if f := funcs[id.Name]; f != nil {
					follow = append(follow, f)
				}
			}
		}
		return true
	})

	if found == WebSocket {
		return WebSocket
	}
	for _, f := range follow {
		if got := detect(f, funcs, seen, depth+1); got != "" {
			if got == WebSocket {
				return WebSocket
			}
			found = got
		}
	}
	return found
}

// isUpgrade recognises a connection being upgraded. Every Go WebSocket library
// spells this differently, but all of them do it in one call:
//
//	gorilla/websocket:  upgrader.Upgrade(w, r, nil)
//	coder/nhooyr:       websocket.Accept(w, r, nil)
//	gobwas/ws:          ws.UpgradeHTTP(r, w)
//
// Upgrade is matched on the method name rather than the receiver's type, so a
// wrapper type around gorilla's Upgrader is still recognised.
func isUpgrade(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Upgrade", "UpgradeHTTP":
		// Two arguments at least (writer and request); this filters out
		// unrelated methods that happen to be called Upgrade.
		return len(call.Args) >= 2
	case "Accept":
		// nhooyr/coder websocket.Accept. Requiring the package name here
		// matters: Accept is also how a net.Listener takes a connection, and
		// that has no arguments at all.
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "websocket" {
			return len(call.Args) >= 2
		}
	}
	return false
}

// isSSE recognises the two ways a handler declares an event stream: setting the
// content type, or using a framework helper that sets it for you.
func isSSE(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	// c.SSEvent(...) (gin) and c.Stream(...) name the protocol themselves.
	if sel.Sel.Name == "SSEvent" {
		return true
	}
	// Header().Set("Content-Type", "text/event-stream")
	if sel.Sel.Name == "Set" && len(call.Args) == 2 {
		key, kok := stringLit(call.Args[0])
		val, vok := stringLit(call.Args[1])
		if kok && vok &&
			strings.EqualFold(key, "Content-Type") &&
			strings.Contains(strings.ToLower(val), "text/event-stream") {
			return true
		}
	}
	return false
}

func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		return "", false
	}
	s := strings.Trim(lit.Value, "`\"")
	return s, true
}

// Describe returns the sentence the console shows for a kind, so the wording
// stays in one place rather than being duplicated into the UI.
func Describe(kind string) string {
	switch kind {
	case WebSocket:
		return "Upgrades the connection to a WebSocket; the response body below does not apply."
	case SSE:
		return "Streams server-sent events and holds the connection open; the response body below does not apply."
	}
	return ""
}

// Endpoint is a realtime route, for the console's Realtime tab.
type Endpoint struct {
	Kind    string `json:"kind"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler,omitempty"`
}

// Collect gathers the realtime routes out of a scan result, so the console can
// list them without walking every operation itself.
func Collect(routes []core.Route) []Endpoint {
	var out []Endpoint
	for _, r := range routes {
		if r.Realtime == "" {
			continue
		}
		out = append(out, Endpoint{
			Kind:    r.Realtime,
			Method:  strings.ToUpper(r.Method),
			Path:    r.Path,
			Handler: r.HandlerName,
		})
	}
	return out
}
