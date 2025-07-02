// Package main implements a CLI tool to bump the version in a Go source file,
// stage changes, commit, and tag using git.
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	goversion "github.com/bcomnes/goversion/v2/pkg"
)

type arrayFlags []string

func (a *arrayFlags) String() string {
	return fmt.Sprint(*a)
}

func (a *arrayFlags) Set(value string) error {
	*a = append(*a, value)
	return nil
}

func usage() {
	msg := `Usage:
  goversion [options] <version-bump>

Bumps the version in a Go source file (default: ./version.go), commits the change with the version string (no "v" prefix),
and tags the commit with the version prefixed with "v". For major version bumps >= v2, go.mod and all self references are also updated.

Examples:
  goversion minor
  goversion 1.2.3
  goversion -bump-file package.json -bump-file Cargo.toml patch

Positional arguments:
  <version-bump>     One of: major, minor, patch, premajor, preminor, prepatch, prerelease, from-git, or an explicit version like 1.2.3

Options:
`
	fmt.Fprint(os.Stderr, msg)
	flag.PrintDefaults()
}

func main() {
	// Define flags.
	versionFile := flag.String("version-file", "./version.go", "Path to the Go file containing the version declaration")
	var extraFiles arrayFlags
	flag.Var(&extraFiles, "file", "Additional file to stage and commit. May be repeated.")
	var bumpFiles arrayFlags
	flag.Var(&bumpFiles, "bump-file", "Additional file to scan for first semver and bump it. May be repeated.")
	dryRun := flag.Bool("dry", false, "Perform a dry run without modifying any files or git repository")
	showVersion := flag.Bool("version", false, "Show CLI version and exit")
	help := flag.Bool("help", false, "Show help message and exit")

	flag.Usage = usage
	flag.Parse()

	if *help {
		usage()
		os.Exit(0)
	}
	if *showVersion {
		fmt.Println("goversion CLI version", Version)
		os.Exit(0)
	}

	// Guard against misplaced flags after positional args.
	for _, arg := range flag.Args() {
		if strings.HasPrefix(arg, "-") {
			fmt.Fprintln(os.Stderr, "Error: Flags must be specified before the command. Please reorder your arguments.")
			usage()
			os.Exit(1)
		}
	}

	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Error: <version-bump> positional argument is required")
		usage()
		os.Exit(1)
	}
	versionArg := args[0]

	// Make sure versionFile is in extraFiles so it's always staged.
	if !slices.Contains(extraFiles, *versionFile) {
		extraFiles = append(extraFiles, *versionFile)
	}

	var meta goversion.VersionMeta
	var err error

	if *dryRun {
		meta, err = goversion.DryRun(*versionFile, versionArg, bumpFiles)
	} else {
		meta, err = goversion.Run(*versionFile, versionArg, extraFiles, bumpFiles)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	// Summary
	if *dryRun {
		fmt.Println("Dry run complete — no files were modified.")
	} else {
		fmt.Println("Version bump successful!")
	}
	fmt.Printf("Old Version: %s\n", meta.OldVersion)
	fmt.Printf("New Version: %s\n", meta.NewVersion)
	fmt.Printf("Bump Type:   %s\n", meta.BumpType)

	// Print out exactly which files were (or would be) touched.
	if len(meta.UpdatedFiles) > 0 {
		if *dryRun {
			fmt.Println("Files that would be updated:")
		} else {
			fmt.Println("Files updated:")
		}
		for _, f := range meta.UpdatedFiles {
			fmt.Printf("  %s\n", f)
		}
	}

}
