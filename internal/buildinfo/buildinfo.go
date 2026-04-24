// Package buildinfo holds version metadata injected by goreleaser at link
// time. A single package lets one set of -X ldflags cover every command
// instead of duplicating vars across each cmd/*/main.go.
package buildinfo

import "fmt"

var (
	// Version is the tag name (e.g. "v0.2.0") when built by goreleaser,
	// or "dev" when built locally by `go build`.
	Version = "dev"

	// Commit is the short git SHA at build time.
	Commit = "none"

	// Date is the build timestamp (RFC3339) from goreleaser.
	Date = "unknown"
)

// String returns a one-line "status-saver <version> (<commit>, <date>)"
// string suitable for the `version` subcommand.
func String() string {
	return fmt.Sprintf("status-saver %s (commit %s, built %s)", Version, Commit, Date)
}
