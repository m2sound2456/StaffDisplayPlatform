// Package version exposes build metadata. The values are injected at link time
// by the Makefile (-ldflags -X ...), see backend/Makefile.
package version

import "runtime"

var (
	// Version is the semantic version of the API binary.
	Version = "0.1.0"
	// GitCommit is the short commit hash the binary was built from.
	GitCommit = "unknown"
	// BuildTime is the RFC3339 UTC timestamp of the build.
	BuildTime = "unknown"
)

// GoVersion reports the Go runtime the binary was compiled with.
func GoVersion() string { return runtime.Version() }

// String returns a compact human readable version, e.g. "0.1.0 (a1b2c3d)".
func String() string { return Version + " (" + GitCommit + ")" }
