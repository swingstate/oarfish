// Package version holds build metadata injected at link time.
package version

// These variables are overridden with -ldflags "-X ..." by the Makefile and CI.
var (
	Version = "dev"
	Commit  = "unknown"
)
