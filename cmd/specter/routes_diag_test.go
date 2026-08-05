package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestCLIWarnsOnDynamicRoute checks that a route whose path cannot be
// statically resolved is reported on stderr, and that -strict-routes turns
// that diagnostic into a non-zero exit while the default run stays exit 0.
func TestCLIWarnsOnDynamicRoute(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-dir", "../../internal/adapter/gin/testdata/dynroute"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("default run should exit 0, got %d\nstderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "dynamic route") {
		t.Errorf("stderr missing dynamic-route warning:\n%s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-strict-routes", "-dir", "../../internal/adapter/gin/testdata/dynroute"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("-strict-routes should exit non-zero when a route is dynamic")
	}
}
