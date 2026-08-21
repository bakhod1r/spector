// Package mount attaches the Spector console to a router the caller already
// built. Nothing here starts a server, opens a port, or spawns a goroutine:
// each function registers routes on the router it is given, so the console
// shares that server's port, middleware, and TLS, and stops when it stops.
//
// One function per framework, all in one package. Importing this package
// therefore compiles every supported framework into your binary. That cost is
// contained — the root spector package does not import this one, so callers
// who wrap spector.Handler themselves pay nothing.
package mount

import (
	"net/http"
	"strings"

	"github.com/bakhod1r/spector"
)

// endpoints are the paths the console serves, relative to the mount point.
// Frameworks without a wildcard route (gin, echo) register each one; the rest
// mount the handler on the whole subtree.
var endpoints = []struct {
	path, method string
}{
	{"/", http.MethodGet},
	{"/openapi.json", http.MethodGet},
	{"/grpc.json", http.MethodGet},
	{"/graphql.json", http.MethodGet},
	{"/source", http.MethodGet},
	{"/grpc/invoke", http.MethodPost},
	// The console fetches these two as well. Leaving them out cost gin and
	// echo users the "Generate body" button and gRPC streaming — features that
	// worked on every framework that mounts the handler on a subtree, so the
	// same console silently differed by router.
	{"/synth/body", http.MethodGet},
	{"/grpc/stream", http.MethodGet},
}

// prepare returns the normalized mount point and the console handler with the
// mount prefix stripped, so the handler sees the same paths regardless of how
// deeply it was mounted.
func prepare(cfg spector.Config) (base string, h http.Handler) {
	base = cfg.BasePathOrDefault()
	return base, http.StripPrefix(strings.TrimSuffix(base, "/"), spector.Handler(cfg))
}

// redirectTarget is where a request for the bare mount point goes. The console
// fetches openapi.json and friends relatively, so the browser has to be on the
// trailing-slash form for those to resolve. The query is carried across so a
// ?key= on the first visit is not lost.
func redirectTarget(base, rawQuery string) string {
	target := base + "/"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	return target
}
