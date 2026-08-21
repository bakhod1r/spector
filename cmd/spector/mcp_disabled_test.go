//go:build !mcp

package main

import (
	"bytes"
	"strings"
	"testing"
)

// A build without the MCP server must say so and say how to get one, rather
// than failing with something the caller cannot act on.
func TestRunMCPWithoutTagExplains(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-mcp"}, &stdout, &stderr); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "-tags mcp") {
		t.Errorf("stderr = %q, want the rebuild instruction", stderr.String())
	}
}
