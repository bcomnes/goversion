package main

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	goversion "github.com/bcomnes/goversion/v2/pkg"
)

type arrayFlags []string

// String returns the accumulated values for display by the flag package.
func (flags *arrayFlags) String() string {
	return fmt.Sprint(*flags)
}

// Set appends one occurrence of a repeatable flag.
func (flags *arrayFlags) Set(value string) error {
	*flags = append(*flags, value)
	return nil
}

// runVersionCommand parses versioning flags, performs the bump, and reports its result.
func runVersionCommand(arguments []string, output, errorOutput io.Writer) int {
	flags := flag.NewFlagSet("goversion", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	workDir := flags.String("workdir", ".", "Go module working directory; relative file and script paths are resolved within it")
	versionFile := flags.String("version-file", "./version.go", "Path to the Go file containing the version declaration")
	var extraFiles arrayFlags
	flags.Var(&extraFiles, "file", "Additional file to stage and commit. May be repeated.")
	var bumpFiles arrayFlags
	flags.Var(&bumpFiles, "bump-file", "Additional file to scan for first semver and bump it. May be repeated.")
	postBump := flags.String("post-bump", "", "Script to execute after version bump but before git commit. Receives GOVERSION_OLD_VERSION and GOVERSION_NEW_VERSION env vars.")
	dryRun := flags.Bool("dry", false, "Perform a dry run without modifying any files or git repository")
	showVersion := flags.Bool("version", false, "Show CLI version and exit")
	help := flags.Bool("help", false, "Show help message and exit")
	flags.Usage = func() { printVersionUsage(flags.Output(), flags) }

	if err := flags.Parse(arguments); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *help {
		printVersionUsage(errorOutput, flags)
		return 0
	}
	if *showVersion {
		fmt.Fprintln(output, "goversion CLI version", Version)
		return 0
	}

	for _, argument := range flags.Args() {
		if strings.HasPrefix(argument, "-") {
			fmt.Fprintln(errorOutput, "Error: Flags must be specified before the command. Please reorder your arguments.")
			printVersionUsage(errorOutput, flags)
			return 1
		}
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(errorOutput, "Error: <version-bump> positional argument is required")
		printVersionUsage(errorOutput, flags)
		return 1
	}

	if !slices.Contains(extraFiles, *versionFile) {
		extraFiles = append(extraFiles, *versionFile)
	}

	var meta goversion.VersionMeta
	var err error
	options := goversion.VersionOptions{
		WorkDir:        *workDir,
		VersionFile:    *versionFile,
		ExtraFiles:     extraFiles,
		BumpFiles:      bumpFiles,
		PostBumpScript: *postBump,
	}
	if *dryRun {
		meta, err = goversion.DryRunWithOptions(options, flags.Arg(0))
	} else {
		meta, err = goversion.RunWithOptions(options, flags.Arg(0))
	}
	if err != nil {
		fmt.Fprintln(errorOutput, "Error:", err)
		return 1
	}

	if *dryRun {
		fmt.Fprintln(output, "Dry run complete — no files were modified.")
	} else {
		fmt.Fprintln(output, "Version bump successful!")
	}
	fmt.Fprintf(output, "Old Version: %s\n", meta.OldVersion)
	fmt.Fprintf(output, "New Version: %s\n", meta.NewVersion)
	fmt.Fprintf(output, "Tag:         %s\n", meta.Tag)
	fmt.Fprintf(output, "Bump Type:   %s\n", meta.BumpType)
	if len(meta.UpdatedFiles) > 0 {
		if *dryRun {
			fmt.Fprintln(output, "Files that would be updated:")
		} else {
			fmt.Fprintln(output, "Files updated:")
		}
		for _, file := range meta.UpdatedFiles {
			fmt.Fprintf(output, "  %s\n", file)
		}
	}
	return 0
}

// printVersionUsage writes help for the version command.
func printVersionUsage(output io.Writer, flags *flag.FlagSet) {
	fmt.Fprint(output, `Usage:
  goversion [options] <version-bump>

Bumps the version in a Go source file (default: ./version.go), commits the change with the version string (no "v" prefix),
and tags the commit with the version prefixed with "v". For major version bumps >= v2, go.mod and all self references are also updated.

Examples:
  goversion minor
  goversion 1.2.3
  goversion -workdir tools/widget patch
  goversion -bump-file docs/version.txt patch
  goversion -post-bump ./scripts/update-docs.sh -file docs/version.md patch

Publishing:
  goversion publish [options]

Positional arguments:
  <version-bump>     One of: major, minor, patch, premajor, preminor, prepatch, prerelease, from-git, or an explicit version like 1.2.3

Options:
`)
	flags.SetOutput(output)
	flags.PrintDefaults()
}
