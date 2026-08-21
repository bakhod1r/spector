package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

// A stamped binary must report the tag it was stamped with and nothing else:
// this is the string a bug report quotes.
func TestBuildVersionPrefersTheStampedTag(t *testing.T) {
	old := version
	version = "v1.2.3"
	defer func() { version = old }()

	if got := buildVersion(); got != "v1.2.3" {
		t.Fatalf("buildVersion() = %q, want v1.2.3", got)
	}
}

// Without ldflags the value comes from the build info the toolchain embeds.
// What it is depends on how the test binary was built, so the assertion is
// that it says something rather than that it says one particular thing.
func TestBuildVersionFallsBackToBuildInfo(t *testing.T) {
	old := version
	version = ""
	defer func() { version = old }()

	got := buildVersion()
	if got == "" {
		t.Fatal("buildVersion() is empty with no stamped tag")
	}
	if strings.TrimSpace(got) != got {
		t.Fatalf("buildVersion() = %q, has surrounding space", got)
	}
}

// The build info cases, which differ by how the binary was produced: an
// installed module version, a tagged working tree, a dirty one, and a build
// with no VCS information at all (the toolchain omits it outside a repository).
func TestVersionFromBuildInfo(t *testing.T) {
	settings := func(kv ...string) []debug.BuildSetting {
		var out []debug.BuildSetting
		for i := 0; i < len(kv); i += 2 {
			out = append(out, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
		}
		return out
	}

	cases := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{
			name: "module version wins",
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.4.0"}},
			want: "v0.4.0",
		},
		{
			name: "revision is shortened",
			info: &debug.BuildInfo{
				Main:     debug.Module{Version: "(devel)"},
				Settings: settings("vcs.revision", "8e4593c3b7370123456789abcdef", "vcs.modified", "false"),
			},
			want: "(devel) 8e4593c3b737",
		},
		{
			name: "uncommitted changes are reported",
			info: &debug.BuildInfo{
				Main:     debug.Module{Version: ""},
				Settings: settings("vcs.revision", "8e4593c3b7370123456789abcdef", "vcs.modified", "true"),
			},
			want: "(devel) 8e4593c3b737-dirty",
		},
		{
			name: "a short revision is left alone",
			info: &debug.BuildInfo{
				Main:     debug.Module{Version: "(devel)"},
				Settings: settings("vcs.revision", "8e4593c"),
			},
			want: "(devel) 8e4593c",
		},
		{
			name: "no VCS information at all",
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want: "(devel)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionFrom(tc.info); got != tc.want {
				t.Fatalf("versionFrom() = %q, want %q", got, tc.want)
			}
		})
	}
}

// -V is a terminating flag: it writes to stdout, exits 0, and must not scan
// anything, so it works in a directory that holds no Go source at all.
func TestVersionFlagPrintsAndExits(t *testing.T) {
	old := version
	version = "v1.2.3"
	defer func() { version = old }()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-V", "-dir", t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "v1.2.3" {
		t.Fatalf("stdout = %q, want v1.2.3", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
