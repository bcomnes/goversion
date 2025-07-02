// Package main implements the goversion CLI tool.
//
// The goversion tool is a command-line interface that automates semantic version bumping
// for Go projects. It reads a version from a specified Go file (default "./version.go"),
// bumps the version according to a given directive (e.g. "patch", "minor", "major", or an
// explicit version string), stages the change, commits it with the bumped version as the commit
// message (without the "v" prefix), and tags the commit with the bumped version (prefixed with "v").
//
// Command Usage:
//
//	goversion [flags] <version-bump>
//
// Flags:
//
//	-version-file: Specifies the path to the Go file containing the version declaration.
//	               (Defaults to "./version.go")
//	-file:         Specifies additional file(s) to be staged together with the version file.
//	               This flag may be used multiple times.
//	-bump-file:    Specifies additional file(s) to scan for the first semantic version and bump it.
//	               This flag may be used multiple times. The found version is replaced with the same
//	               version as the main version file. Only valid semver strings are matched (no "v" prefix).
//	-post-bump:    Specifies a script to execute after version bump but before git commit.
//	               The script receives GOVERSION_OLD_VERSION and GOVERSION_NEW_VERSION environment variables.
//	               Files created or modified by the script must be specified with -file to be included in the commit.
//	-version:      Displays the version of the goversion CLI tool and exits.
//
// Examples:
//
//	# Bump the patch version (e.g. 1.2.3 → 1.2.4)
//	goversion patch
//
//	# Bump the minor version (e.g. 1.2.3 → 1.3.0)
//	goversion minor
//
//	# Bump the major version (e.g. 1.2.3 → 2.0.0)
//	goversion major
//
//	# Create a prerelease version (e.g. 1.2.3 → 1.2.4-0)
//	goversion prerelease
//
//	# Bump a prerelease version (e.g. 1.2.4-0 → 1.2.4-1)
//	goversion prerelease
//
//	# Set an explicit version directly
//	goversion 2.1.0
//
//	# Set a prerelease version explicitly
//	goversion 2.1.0-beta.1
//
//	# Use a version from the latest Git tag
//	goversion from-git
//
//	# Bump patch version and include README.md in the commit
//	goversion -version-file=./version.go -file=README.md patch
//
//	# Bump version in multiple files (package.json, Cargo.toml, etc.)
//	goversion -bump-file=package.json -bump-file=Cargo.toml patch
//
//	# Run a script after bumping but before committing
//	# Files created by the script must be explicitly included with -file
//	goversion -post-bump=./scripts/update-docs.sh -file=docs/version.md minor
//
//	# Combine version file, bump files, and extra files
//	goversion -version-file=./version.go -bump-file=package.json -file=README.md minor
//
// This command bumps the patch version, updates the version file, stages the changes
// (including README.md), commits using the new version as the commit message, and tags
// the commit with the new version.
//
// For more detailed API documentation, please see the documentation in the "pkg" package
// or visit [PkgGoDev](https://pkg.go.dev/github.com/bcomnes/goversion/v2).
package main
