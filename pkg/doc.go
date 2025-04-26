// Package goversion provides a library for managing semantic version bumps in Go projects.
//
// It provides functionalities for:
//   - Reading and writing a version file that contains a version constant.
//   - Normalizing and parsing semantic version strings (ensuring a canonical "v" prefix).
//   - Bumping versions using standard keywords (e.g., major, minor, patch, premajor, preminor, prepatch, prerelease, and from-git)
//     or setting an explicit version.
//   - Integrating with Git to stage changes, commit updates with the new version as the commit message (without the "v" prefix),
//     and tag commits with the new version (prefixed with "v").
//
// This library is designed to be used both as a standalone command-line tool via the provided CLI (in the cmd folder)
// and as a programmatic API to integrate version bumping into other Go programs.
//
// Usage Example:
//
//	import (
//	    "log"
//	    "github.com/bcomnes/goversion/pkg"
//	)
//
//	func main() {
//	    // Bump the version by "patch".
//	    err := goversion.Run("./version.go", "patch", []string{"./version.go"})
//	    if err != nil {
//	        log.Fatalf("version bump failed: %v", err)
//	    }
//	    log.Println("Version bumped successfully!")
//	}
//
// For additional details and API documentation, see https://pkg.go.dev/github.com/bcomnes/goversion.
package goversion
