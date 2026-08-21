//go:build !mcp

package main

import (
	"fmt"
	"io"
)

// The MCP server is behind the mcp build tag for the same reason live gRPC is
// behind grpclive: it is a dependency an install pays for whether or not it is
// ever used. Serving spector to an editor's agent is worth the five megabytes
// to the people who do it, and nothing to everyone else.
func runMCP(stderr io.Writer) int {
	fmt.Fprintln(stderr, "spector: this build has no MCP server")
	fmt.Fprintln(stderr, "spector: install it with: go install -tags mcp github.com/bakhod1r/spector/cmd/spector@latest")
	return 1
}
