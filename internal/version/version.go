// Package version exposes build-time version metadata.
package version

var (
	// Version is the release version, injected at build time.
	Version = "dev"
	// Commit is the git commit, injected at build time.
	Commit = "none"
	// BuildDate is the UTC build timestamp, injected at build time.
	BuildDate = "unknown"
)

// String returns a human-readable version string.
func String() string {
	return Version
}
