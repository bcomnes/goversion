# goversion
[![Actions Status][action-img]][action-url]
[![PkgGoDev][pkg-go-dev-img]][pkg-go-dev-url]

[action-img]: https://github.com/bcomnes/goversion/actions/workflows/test.yml/badge.svg
[action-url]: https://github.com/bcomnes/goversion/actions/workflows/test.yml
[pkg-go-dev-img]: https://pkg.go.dev/badge/github.com/bcomnes/goversion
[pkg-go-dev-url]: https://pkg.go.dev/github.com/bcomnes/goversion

`goversion` is a tool and library for managing semantic version bumps in your Go projects. It automates updating a version file, staging changes, committing, and tagging via Git—all through a simple CLI and programmatic API.

## Features

- **Semantic Version Bumping:** Support for bumping versions using keywords (major, minor, patch, premajor, preminor, prepatch, prerelease, and from-git) or setting an explicit version.
- **Git Integration:** Automatically stages updated files, commits changes with the new version as the commit message, and tags the commit with the new version.
- **CLI and Library:** Offers both a command-line interface for quick version updates and a library for integrating version management into your applications.
- **Flexible Configuration:** Specify the path to your version file and include additional files for Git staging.

## Install

Install via Go modules:

```console
go get github.com/bcomnes/goversion
```

## Usage

### Command-Line Interface

The `goversion` CLI defaults to using `pkg/version.go` as the version file, but you can override this with the `-version-file` flag. Use the `-file` flag to specify additional files to be staged.

```
goversion [flags] <version-bump>
```

#### Flags

- `-version-file`: Path to the Go file containing the version declaration. (Default: `pkg/version.go`)
- `-file`: Additional file to include in the commit. This flag can be used multiple times.
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
```

This command will:
- Bump the version in the given file.
- Stage the updated version file (plus any `-file` flags).
- Commit with the new version as the commit message (no `v` prefix).
- Tag the commit with the new version (with `v` prefix).

> **Note**: The working directory must be clean (no unstaged/uncommitted changes outside the listed files) or the command will fail to prevent accidental commits.

### Library Usage

You can also integrate goversion into your Go programs. For example:

```go
package main

import (
	"fmt"
	"log"

	"github.com/bcomnes/goversion/pkg"
)

func main() {
	err := pkg.Run("pkg/version/version.go", "minor", []string{"pkg/version/version.go"})
	if err != nil {
		log.Fatalf("version bump failed: %v", err)
	}
	fmt.Println("Version bumped successfully!")
}
```

## API Documentation

For detailed API documentation, visit [PkgGoDev][pkg-go-dev-url].

## License

This project is licensed under the MIT License.
