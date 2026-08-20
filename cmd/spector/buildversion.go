package main

import (
	"runtime/debug"
	"strings"
)

// version is stamped by the release build with -ldflags "-X main.version=v1.2.3".
// It is empty in every other build, which is the case buildVersion handles.
var version string

// buildVersion reports the version of the binary itself, as opposed to the
// -version flag, which is the version of the API being documented.
//
// A released binary carries a stamped tag. A `go install ...@v0.4.0` build
// carries no ldflags at all, but the module version is recorded in the build
// info, so it is read from there. A `go build` from a working tree has
// neither: it reports the VCS revision the toolchain embeds, which is what
// identifies a build nobody tagged.
func buildVersion() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	return versionFrom(info)
}

// versionFrom is the half of buildVersion that reads the build info, split out
// because debug.ReadBuildInfo describes the running test binary and cannot be
// made to describe anything else: every branch below is reachable in a real
// build and unreachable through buildVersion in a test.
func versionFrom(info *debug.BuildInfo) string {
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}

	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if revision == "" {
		return "(devel)"
	}
	// A short hash is what a person compares against a commit listing; the
	// full 40 characters carry no extra meaning here.
	if len(revision) > 12 {
		revision = revision[:12]
	}
	var b strings.Builder
	b.WriteString("(devel) ")
	b.WriteString(revision)
	if modified == "true" {
		// Uncommitted changes mean the hash alone does not describe the
		// binary, and a bug report that omits this is unreproducible.
		b.WriteString("-dirty")
	}
	return b.String()
}
