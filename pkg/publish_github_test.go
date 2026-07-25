package goversion

import (
	"errors"
	"strings"
	"testing"
)

func TestPublishGitHubUnavailable(t *testing.T) {
	tests := []struct {
		name     string
		lookErr  error
		commands []publishTestCommand
		warning  string
	}{
		{
			name:    "gh missing",
			lookErr: errors.New("not found"),
			warning: "install GitHub CLI",
		},
		{
			name: "gh unauthenticated",
			commands: []publishTestCommand{
				{name: "gh", args: []string{"auth", "status", "--hostname", "example.com"}, out: "not logged in", err: errors.New("exit 1")},
			},
			warning: "gh auth login",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &publishTestRunner{t: t, commands: test.commands, lookErr: test.lookErr}
			meta := PublishMeta{Tag: "v1.2.3", Version: "v1.2.3", ReleaseStatus: PublishStepPending}
			if err := publishGitHubRelease(t.TempDir(), "git@example.com:acme/tool.git", &meta, runner, false, false); err != nil {
				t.Fatalf("publishGitHubRelease returned error: %v", err)
			}
			runner.done()
			if meta.ReleaseStatus != PublishStepSkipped || len(meta.Warnings) != 1 || !strings.Contains(meta.Warnings[0], test.warning) {
				t.Fatalf("unexpected release metadata: %+v", meta)
			}
		})
	}
}

func TestPublishGitHubReleaseStatuses(t *testing.T) {
	const (
		tag        = "v1.2.3"
		remoteURL  = "git@example.com:acme/tool.git"
		repo       = "example.com/acme/tool"
		releaseURL = "https://example.com/acme/tool/releases/tag/v1.2.3"
	)
	baseCommands := func() []publishTestCommand {
		return []publishTestCommand{
			{name: "gh", args: []string{"auth", "status", "--hostname", "example.com"}},
			{name: "gh", args: []string{"repo", "view", remoteURL, "--json", "nameWithOwner", "--jq", ".nameWithOwner"}, out: "acme/tool\n"},
		}
	}
	tests := []struct {
		name       string
		disabled   bool
		dryRun     bool
		commands   []publishTestCommand
		wantStatus PublishStepStatus
		wantURL    string
		wantErr    string
	}{
		{
			name:       "disabled",
			disabled:   true,
			wantStatus: PublishStepSkipped,
		},
		{
			name:   "dry run plans missing release",
			dryRun: true,
			commands: append(baseCommands(),
				publishTestCommand{name: "gh", args: []string{"release", "view", tag, "--repo", repo, "--json", "url", "--jq", ".url"}, out: "release not found\n", err: errors.New("not found")},
			),
			wantStatus: PublishStepPlanned,
		},
		{
			name:   "dry run reuses existing release",
			dryRun: true,
			commands: append(baseCommands(),
				publishTestCommand{name: "gh", args: []string{"release", "view", tag, "--repo", repo, "--json", "url", "--jq", ".url"}, out: releaseURL + "\n"},
			),
			wantStatus: PublishStepReused,
			wantURL:    releaseURL,
		},
		{
			name: "creates release",
			commands: append(baseCommands(),
				publishTestCommand{name: "gh", args: []string{"release", "view", tag, "--repo", repo, "--json", "url", "--jq", ".url"}, out: "release not found\n", err: errors.New("not found")},
				publishTestCommand{name: "gh", args: []string{"release", "create", tag, "--verify-tag", "--generate-notes", "--title", tag, "--repo", repo}, out: releaseURL + "\n"},
			),
			wantStatus: PublishStepCompleted,
			wantURL:    releaseURL,
		},
		{
			name: "release lookup error",
			commands: append(baseCommands(),
				publishTestCommand{name: "gh", args: []string{"release", "view", tag, "--repo", repo, "--json", "url", "--jq", ".url"}, out: "connection reset\n", err: errors.New("exit 1")},
			),
			wantStatus: PublishStepPending,
			wantErr:    "inspect GitHub release",
		},
		{
			name: "release creation error",
			commands: append(baseCommands(),
				publishTestCommand{name: "gh", args: []string{"release", "view", tag, "--repo", repo, "--json", "url", "--jq", ".url"}, out: "release not found\n", err: errors.New("not found")},
				publishTestCommand{name: "gh", args: []string{"release", "create", tag, "--verify-tag", "--generate-notes", "--title", tag, "--repo", repo}, out: "permission denied\n", err: errors.New("exit 1")},
			),
			wantStatus: PublishStepPending,
			wantErr:    "create GitHub release",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &publishTestRunner{t: t, commands: test.commands}
			meta := PublishMeta{Tag: tag, Version: tag, ReleaseStatus: PublishStepPending}
			err := publishGitHubRelease(t.TempDir(), remoteURL, &meta, runner, test.disabled, test.dryRun)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
				}
			} else if err != nil {
				t.Fatalf("publishGitHubRelease returned error: %v", err)
			}
			runner.done()
			if meta.ReleaseStatus != test.wantStatus || meta.ReleaseURL != test.wantURL {
				t.Fatalf("unexpected release metadata: %+v", meta)
			}
		})
	}
}

func TestPublishGitHubReleaseCreateRaceReusesRelease(t *testing.T) {
	const releaseURL = "https://github.com/acme/tool/releases/tag/v1.2.3"
	viewArgs := []string{"release", "view", "v1.2.3", "--repo", "github.com/acme/tool", "--json", "url", "--jq", ".url"}
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "gh", args: []string{"auth", "status", "--hostname", "github.com"}},
		{name: "gh", args: []string{"repo", "view", "git@github.com:acme/tool.git", "--json", "nameWithOwner", "--jq", ".nameWithOwner"}, out: "acme/tool\n"},
		{name: "gh", args: viewArgs, out: "release not found\n", err: errors.New("not found")},
		{name: "gh", args: []string{"release", "create", "v1.2.3", "--verify-tag", "--generate-notes", "--title", "v1.2.3", "--repo", "github.com/acme/tool"}, out: "release already exists\n", err: errors.New("exit 1")},
		{name: "gh", args: viewArgs, out: releaseURL + "\n"},
	}}
	meta := PublishMeta{Tag: "v1.2.3", Version: "v1.2.3", ReleaseStatus: PublishStepPending}

	if err := publishGitHubRelease(t.TempDir(), "git@github.com:acme/tool.git", &meta, runner, false, false); err != nil {
		t.Fatalf("publishGitHubRelease returned error: %v", err)
	}
	runner.done()
	if meta.ReleaseStatus != PublishStepReused || meta.ReleaseURL != releaseURL {
		t.Fatalf("unexpected release metadata: %+v", meta)
	}
}
