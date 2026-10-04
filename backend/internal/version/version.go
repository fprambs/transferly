// Package version holds the version of Transferly, which releases set when they build the binary.
package version

// Version is the version of the build, such as v0.5.0, given by the release tag through -ldflags
// "-X github.com/fprambs/transferly/internal/version.Version=v0.5.0"; builds that do not set it are dev.
var Version = "dev"
