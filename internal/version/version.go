package version

import "fmt"

// These variables are injected at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String returns a human-readable version string.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}

// UserAgent returns a User-Agent header value for HTTP requests.
func UserAgent() string {
	return fmt.Sprintf("Spotlight/%s", Version)
}
