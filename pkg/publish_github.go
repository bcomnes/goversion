package goversion

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"golang.org/x/mod/semver"
)

func publishGitHubRelease(workDir, remoteURL string, meta *PublishMeta, runner publishCommandRunner, disabled, dryRun bool) error {
	if disabled {
		meta.ReleaseStatus = PublishStepSkipped
		return nil
	}
	if _, err := runner.LookPath("gh"); err != nil {
		meta.ReleaseStatus = PublishStepSkipped
		meta.Warnings = append(meta.Warnings, "GitHub release skipped because gh was not found; install GitHub CLI and run gh auth login before publishing the release")
		return nil
	}

	authArgs := []string{"auth", "status"}
	if host := gitRemoteHost(remoteURL); host != "" {
		authArgs = append(authArgs, "--hostname", host)
	}
	authOutput, err := runner.Run(workDir, nil, "gh", authArgs...)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return publishCommandError("check GitHub authentication", authOutput, err, "gh", authArgs...)
		}
		meta.ReleaseStatus = PublishStepSkipped
		warning := "GitHub release skipped because gh authentication failed; run gh auth login and retry"
		if detail := strings.TrimSpace(string(authOutput)); detail != "" {
			warning += ": " + detail
		}
		meta.Warnings = append(meta.Warnings, warning)
		return nil
	}

	repoArgs := []string{"repo", "view", remoteURL, "--json", "nameWithOwner", "--jq", ".nameWithOwner"}
	repoOutput, err := runner.Run(workDir, nil, "gh", repoArgs...)
	if err != nil {
		return publishCommandError("resolve GitHub repository for remote", repoOutput, err, "gh", repoArgs...)
	}
	repo := strings.TrimSpace(string(repoOutput))
	if repo == "" {
		return errors.New("gh repo view returned an empty repository name")
	}
	if host := gitRemoteHost(remoteURL); host != "" {
		repo = host + "/" + repo
	}

	viewArgs := []string{"release", "view", meta.Tag, "--repo", repo, "--json", "url", "--jq", ".url"}
	viewOutput, viewErr := runner.Run(workDir, nil, "gh", viewArgs...)
	if viewErr == nil {
		meta.ReleaseStatus = PublishStepReused
		return recordReleaseURL(meta, viewOutput)
	}
	if !githubReleaseNotFound(viewOutput) {
		return publishCommandError("inspect GitHub release", viewOutput, viewErr, "gh", viewArgs...)
	}
	if dryRun {
		meta.ReleaseStatus = PublishStepPlanned
		return nil
	}

	createArgs := []string{"release", "create", meta.Tag, "--verify-tag", "--generate-notes", "--title", meta.Tag, "--repo", repo}
	if semver.Prerelease(meta.Version) != "" {
		createArgs = append(createArgs, "--prerelease")
	}
	createOutput, err := runner.Run(workDir, nil, "gh", createArgs...)
	if err == nil {
		meta.ReleaseStatus = PublishStepCompleted
		return recordReleaseURL(meta, createOutput)
	}
	if strings.Contains(strings.ToLower(string(createOutput)), "already exists") {
		viewOutput, viewErr = runner.Run(workDir, nil, "gh", viewArgs...)
		if viewErr == nil {
			meta.ReleaseStatus = PublishStepReused
			return recordReleaseURL(meta, viewOutput)
		}
	}
	return publishCommandError("create GitHub release", createOutput, err, "gh", createArgs...)
}

func recordReleaseURL(meta *PublishMeta, output []byte) error {
	meta.ReleaseURL = strings.TrimSpace(string(output))
	if meta.ReleaseURL == "" {
		return errors.New("gh release command returned an empty release URL")
	}
	return nil
}

func githubReleaseNotFound(output []byte) bool {
	detail := strings.ToLower(string(output))
	return strings.Contains(detail, "release not found") || strings.Contains(detail, "http 404")
}

func gitRemoteHost(remote string) string {
	if parsed, err := url.Parse(remote); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	left, _, found := strings.Cut(remote, ":")
	if !found {
		return ""
	}
	if _, host, found := strings.Cut(left, "@"); found {
		return host
	}
	return left
}
