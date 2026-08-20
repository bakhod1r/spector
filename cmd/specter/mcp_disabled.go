//go:build !mcp

package main

import (
	"fmt"
	"io"
)

// The MCP server is behind the mcp build tag for the same reason live gRPC is
// behind grpclive: it is a dependency an install pays for whether or not it is
// ever used. Serving specter to an editor's agent is worth the five megabytes
// to the people who do it, and nothing to everyone else.
func runMCP(stderr io.Writer) int {
	fmt.Fprintln(stderr, "specter: this build has no MCP server")
	fmt.Fprintln(stderr, "specter: install it with: go install -tags mcp github.com/bakhod1r/spector/cmd/specter@latest")
	return 1
}
