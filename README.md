# goversion
[![Actions Status][action-img]][action-url]
[![SocketDev][socket-image]][socket-url]
[![PkgGoDev][pkg-go-dev-img]][pkg-go-dev-url]

[action-img]: https://github.com/bcomnes/goversion/actions/workflows/test.yml/badge.svg
[action-url]: https://github.com/bcomnes/goversion/actions/workflows/test.yml
[pkg-go-dev-img]: https://pkg.go.dev/badge/github.com/bcomnes/goversion/v2
[pkg-go-dev-url]: https://pkg.go.dev/github.com/bcomnes/goversion/v2
[socket-image]: https://socket.dev/api/badge/go/package/github.com/bcomnes/goversion?version=v1.0.2
[socket-url]: https://socket.dev/go/package/github.com/bcomnes/goversion?version=v1.0.2

`goversion` is a tool and library for managing semantic version bumps in your Go projects. It bumps a `version.go` file while creating  a semantic version commit and tag.
It is intended for use with `go tool`s that are consumed from source.

- `go generate` can generate go code from git commits, but it's too late to capture the current git tag into src during the generate step.
- Build flags are useful for inserting version tags from git into binaries, however `go tool` consumes source code and not binaries.
- By bumping `version.go` before creating the version commit, tools consumed by `go tool` are able to introspect version data using a simple and clean workflow

## Features

- **Semantic Version Bumping:** Support for bumping versions using keywords (major, minor, patch, premajor, preminor, prepatch, prerelease, and from-git) or setting an explicit version.
- **Git Integration:** Automatically stages updated files, commits changes with the new version as the commit message, and tags the commit with the new version.
- **CLI and Library:** Offers both a command-line interface for quick version updates and a library for integrating version management into your applications.
- **Flexible Configuration:** Specify the path to your version file and include additional files for Git staging.
- **go.mod and package self reference updates** Save yourself the hassle of updating the go.mod and pacakge self references.
- **Generic Version Bumping:** Bump versions in any text file (package.json, Cargo.toml, etc.) by finding and replacing the first semantic version.
- **Post-bump Hooks:** Run custom scripts after version bumping but before committing, with access to old and new version via environment variables.

## Install

Install via Go modules:

```console
# Install go version as a tool
go get -tool github.com/bcomnes/goversion/v2
```

## Usage

### Command-Line Interface

The `goversion` CLI defaults to using `./version.go` as the version file, but you can override this with the `-version-file` flag. Use the `-file` flag to specify additional files to be staged.

```
go tool github.com/bcomnes/goversion [flags] <version-bump>
```

#### Flags

- `-version-file`: Path to the Go file containing the version declaration. (Default: `./version.go`)
- `-file`: Additional file to include in the commit. This flag can be used multiple times.
- `-bump-file`: Additional file to scan for the first semantic version and bump it. This flag can be used multiple times. Only valid semver strings are matched (no "v" prefix).
- `-post-bump`: Script to execute after version bump but before git commit. Receives `GOVERSION_OLD_VERSION` and `GOVERSION_NEW_VERSION` environment variables. Files created or modified by the script must be specified with `-file` to be included in the commit.
- `-version`: Show the version of the `goversion` CLI tool and exit.
- `-help`: Show usage instructions.

#### Bump Directives

The `<version-bump>` argument can be:

- **Keywords for semantic bumps:**
  - `major` – 1.2.3 → 2.0.0
  - `minor` – 1.2.3 → 1.3.0
  - `patch` – 1.2.3 → 1.2.4
  - `premajor` – 1.2.3 → 2.0.0-0
  - `preminor` – 1.2.3 → 1.3.0-0
  - `prepatch` – 1.2.3 → 1.2.4-0
  - `prerelease` – 1.2.3 → 1.2.4-0 (or bumps prerelease: 1.2.4-0 → 1.2.4-1)

- **Special source:**
  - `from-git` – use the latest Git tag (e.g. `v1.2.3`) as the version.

- **Explicit version strings (must be valid semver):**
  - `1.2.3` – set exact version
  - `2.0.0-alpha.1` – set prerelease version
  - `dev` – special non-semver string that initializes the version file (used for bootstrapping)

#### Generic Version Bumping

The `-bump-file` flag allows you to bump versions in any text file by finding and replacing the first valid semantic version:

- Only matches strict semver format (no "v" prefix)
- Replaces only the first occurrence
- Works with any file format (JSON, TOML, YAML, etc.)
- Common use cases: package.json, Cargo.toml, pyproject.toml, extension manifests

#### Post-bump Scripts

The `-post-bump` flag runs a script after version bumping but before committing:

- Script receives environment variables:
  - `GOVERSION_OLD_VERSION` - the version before bumping
  - `GOVERSION_NEW_VERSION` - the new version after bumping
- Script output is displayed to the user
- If the script fails, the entire operation is aborted
- Files created/modified by the script must be explicitly included with `-file`
- Common use cases: generating docs, updating changelogs, building artifacts

#### Examples

```console
# Bump patch version (1.2.3 → 1.2.4)
goversion patch

# Bump minor version (1.2.3 → 1.3.0)
goversion minor

# Bump pre-release version (1.2.4-0 → 1.2.4-1)
goversion prerelease

# Set an explicit version
goversion 2.0.0

# Set a prerelease version
goversion 2.1.0-beta.1

# Use version from Git tag
goversion from-git

# Include README.md in the commit
goversion -file=README.md patch

# Use a custom version file path
goversion -version-file=internal/version.go minor

# Bump version in package.json and Cargo.toml
goversion -bump-file=package.json -bump-file=Cargo.toml patch

# Run a post-bump script that generates docs
# Note: Files created by the script must be included with -file
goversion -post-bump=./scripts/update-docs.sh -file=docs/version.md minor

# Combine multiple features
goversion -version-file=./version.go -bump-file=package.json -post-bump=./update.sh -file=CHANGELOG.md patch
```

This command will:
- Bump the version in the given file.
- Stage the updated version file (plus any `-file` flags).
- Commit with the new version as the commit message (no `v` prefix).
- Tag the commit with the new version (with `v` prefix).
- For major version bumps ≥ v2, update go.mod module path and rewrite self-imports.

> **Note**: The working directory must be clean (no unstaged/uncommitted changes outside the listed files) or the command will fail to prevent accidental commits.

### Library Usage

You can also integrate goversion into your Go programs. For example:

```go
package main

import (
	"fmt"
	"log"

	"github.com/bcomnes/goversion/v2/pkg"
)

func main() {
	// Basic version bump
	meta, err := goversion.Run("./version.go", "minor", []string{"./version.go"}, []string{}, "")
	if err != nil {
		log.Fatalf("version bump failed: %v", err)
	}
	fmt.Printf("Bumped from %s to %s\n", meta.OldVersion, meta.NewVersion)

	// With additional files to bump
	bumpFiles := []string{"package.json", "Cargo.toml"}
	meta, err = goversion.Run("./version.go", "patch", []string{"./version.go"}, bumpFiles, "")
	if err != nil {
		log.Fatalf("version bump failed: %v", err)
	}

	// Dry run to see what would change
	meta, err = goversion.DryRun("./version.go", "major", []string{"package.json"})
	if err != nil {
		log.Fatalf("dry run failed: %v", err)
	}
	fmt.Printf("Would update files: %v\n", meta.UpdatedFiles)
}
```

## API Documentation

For detailed API documentation, visit [PkgGoDev][pkg-go-dev-url].

## License

This project is licensed under the MIT License.
