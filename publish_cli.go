package main

import (
	"flag"
	"fmt"
	"io"

	goversion "github.com/bcomnes/goversion/v2/pkg"
)

func runPublishCommand(arguments []string, output, errorOutput io.Writer) int {
	flags := flag.NewFlagSet("goversion publish", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	versionFile := flags.String("version-file", "./version.go", "Path to the Go file containing the published version")
	workDir := flags.String("workdir", ".", "Go module working directory")
	remote := flags.String("remote", "origin", "Git remote to publish to")
	proxy := flags.String("proxy", "https://proxy.golang.org", "Go module proxy to seed after publishing")
	dryRun := flags.Bool("dry", false, "Validate and preview publishing without changing remote state")
	noProxy := flags.Bool("no-proxy", false, "Push and create the GitHub release without seeding a Go module proxy")
	noRelease := flags.Bool("no-release", false, "Push and seed the Go proxy without creating a GitHub Release")
	help := flags.Bool("help", false, "Show publish help and exit")
	flags.Usage = func() { printPublishUsage(flags.Output(), flags) }

	if err := flags.Parse(arguments); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *help {
		printPublishUsage(output, flags)
		return 0
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errorOutput, "Error: goversion publish does not accept positional arguments")
		printPublishUsage(errorOutput, flags)
		return 2
	}

	meta, err := goversion.Publish(goversion.PublishOptions{
		WorkDir:     *workDir,
		VersionFile: *versionFile,
		Remote:      *remote,
		Proxy:       *proxy,
		DryRun:      *dryRun,
		NoProxy:     *noProxy,
		NoRelease:   *noRelease,
	})
	if err != nil {
		fmt.Fprintln(errorOutput, "Error:", err)
		return 1
	}
	for _, warning := range meta.Warnings {
		fmt.Fprintln(errorOutput, "Warning:", warning)
	}

	if *dryRun {
		fmt.Fprintln(output, "Publish dry run successful; no remote state was changed.")
	} else {
		fmt.Fprintln(output, "Go module published successfully!")
	}
	fmt.Fprintf(output, "Module:  %s\n", meta.ModulePath)
	fmt.Fprintf(output, "Version: %s\n", meta.Version)
	fmt.Fprintf(output, "Tag:     %s\n", meta.Tag)
	fmt.Fprintf(output, "Commit:  %s\n", meta.HeadCommit)
	fmt.Fprintf(output, "Remote:  %s\n", meta.Remote)
	printPublishStep(output, "Branch", meta.BranchStatus, "publish branch", "published", "already current")
	printPublishStep(output, "Tag", meta.TagStatus, "publish tag", "published", "already current")
	printReleaseStep(output, meta)
	printProxyStep(output, meta, *proxy)
	return 0
}

func printPublishStep(output io.Writer, label string, status goversion.PublishStepStatus, planned, completed, reused string) {
	detail := string(status)
	switch status {
	case goversion.PublishStepPlanned:
		detail = "would " + planned
	case goversion.PublishStepCompleted:
		detail = completed
	case goversion.PublishStepReused:
		detail = reused
	case goversion.PublishStepSkipped:
		detail = "skipped"
	}
	fmt.Fprintf(output, "%-8s %s\n", label+":", detail)
}

func printReleaseStep(output io.Writer, meta goversion.PublishMeta) {
	detail := string(meta.ReleaseStatus)
	switch meta.ReleaseStatus {
	case goversion.PublishStepPlanned:
		detail = "would create through gh"
	case goversion.PublishStepCompleted:
		detail = "created at " + meta.ReleaseURL
	case goversion.PublishStepReused:
		detail = "already exists at " + meta.ReleaseURL
	case goversion.PublishStepSkipped:
		detail = "skipped"
	}
	fmt.Fprintf(output, "%-8s %s\n", "Release:", detail)
}

func printProxyStep(output io.Writer, meta goversion.PublishMeta, proxy string) {
	detail := string(meta.ProxyStatus)
	switch meta.ProxyStatus {
	case goversion.PublishStepPlanned:
		detail = "would seed " + proxy
	case goversion.PublishStepCompleted:
		detail = "seeded " + proxy
	case goversion.PublishStepSkipped:
		detail = "skipped"
	}
	fmt.Fprintf(output, "%-8s %s\n", "Proxy:", detail)
	if meta.ProxyStatus != goversion.PublishStepSkipped {
		fmt.Fprintf(output, "%-8s https://pkg.go.dev/%s@%s\n", "Docs:", meta.ModulePath, meta.Version)
	}
}

func printPublishUsage(output io.Writer, flags *flag.FlagSet) {
	fmt.Fprintln(output, `Usage:
  goversion publish [options]

Publishes an existing goversion commit and tag as a Go module.
The command atomically publishes incomplete Git refs, creates or reuses a GitHub Release through gh when available, and seeds the Go module proxy.

Run goversion <version-bump>, validate the local release, then run goversion publish.

Options:`)
	flags.SetOutput(output)
	flags.PrintDefaults()
}
