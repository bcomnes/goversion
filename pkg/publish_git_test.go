package goversion

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidatePublishGitRefsRejectsNonFastForward(t *testing.T) {
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "git", args: []string{"push", "--dry-run", "--atomic", "origin", "HEAD:refs/heads/main"}, out: "non-fast-forward\n", err: fmt.Errorf("exit 1")},
	}}
	meta := PublishMeta{Remote: "origin", Branch: "main", Tag: "v1.2.3", HeadCommit: "head"}
	state := publishGitState{remoteBranchCommit: "other", remoteTagCommit: "head"}

	err := validatePublishGitRefs(t.TempDir(), &meta, state, runner)
	if err == nil || !strings.Contains(err.Error(), "validate Git ref publication") || !strings.Contains(err.Error(), "non-fast-forward") {
		t.Fatalf("expected non-fast-forward dry-run error, got %v", err)
	}
	runner.done()
}

func TestPublishGitCheckpoints(t *testing.T) {
	head := "1111111111111111111111111111111111111111"
	other := "ffffffffffffffffffffffffffffffffffffffff"
	tag := "v1.2.3"
	tests := []struct {
		name         string
		remoteBranch string
		remoteTag    string
		pushRefs     []string
		wantBranch   PublishStepStatus
		wantTag      PublishStepStatus
		wantErr      string
	}{
		{
			name:       "both refs missing",
			pushRefs:   []string{"HEAD:refs/heads/main", "refs/tags/v1.2.3:refs/tags/v1.2.3"},
			wantBranch: PublishStepCompleted,
			wantTag:    PublishStepCompleted,
		},
		{
			name:         "branch current tag missing",
			remoteBranch: head,
			pushRefs:     []string{"refs/tags/v1.2.3:refs/tags/v1.2.3"},
			wantBranch:   PublishStepReused,
			wantTag:      PublishStepCompleted,
		},
		{
			name:       "tag current branch missing",
			remoteTag:  head,
			pushRefs:   []string{"HEAD:refs/heads/main"},
			wantBranch: PublishStepCompleted,
			wantTag:    PublishStepReused,
		},
		{
			name:         "both refs current",
			remoteBranch: head,
			remoteTag:    head,
			wantBranch:   PublishStepReused,
			wantTag:      PublishStepReused,
		},
		{
			name:      "conflicting tag",
			remoteTag: other,
			wantErr:   "remote tag v1.2.3 on origin resolves to " + other + ", not HEAD " + head,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := writePublishFixture(t, "example.com/acme/tool", "1.2.3")
			runner := preflightRunnerWithRefs(t, dir, head, tag, "git@example.com:acme/tool.git", test.remoteTag, test.remoteBranch)
			if test.wantErr != "" {
				runner.commands = runner.commands[:len(runner.commands)-1]
			} else {
				if len(test.pushRefs) != 0 {
					args := append([]string{"push", "--atomic", "origin"}, test.pushRefs...)
					runner.commands = append(runner.commands, publishTestCommand{name: "git", args: args})
				}
				runner.commands = append(runner.commands, remoteTagCommand(tag, head), remoteBranchCommand(head))
			}

			meta, err := publish(PublishOptions{WorkDir: dir, NoRelease: true, NoProxy: true}, runner)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
				}
				runner.done()
				return
			}
			if err != nil {
				t.Fatalf("Publish returned error: %v", err)
			}
			runner.done()
			assertPublishStatuses(t, meta, test.wantBranch, test.wantTag, PublishStepSkipped, PublishStepSkipped)
		})
	}
}
