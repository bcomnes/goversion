package goversion

import (
	"errors"
	"strings"
	"testing"
)

func TestPublishMajorBranchDryRunUsesAbsentLeaseWithoutLocalMutation(t *testing.T) {
	head := "1111111111111111111111111111111111111111"
	ref := "refs/heads/v2"
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "git", args: []string{"push", "--dry-run", "--force-with-lease=" + ref + ":", "upstream", head + ":" + ref}},
	}}
	meta := PublishMeta{Remote: "upstream", MajorBranch: "v2", HeadCommit: head, MajorBranchStatus: PublishStepPlanned}

	if err := publishMajorBranch(t.TempDir(), &meta, publishGitState{}, runner, true, true); err != nil {
		t.Fatalf("publishMajorBranch dry run returned error: %v", err)
	}
	runner.done()
	if meta.MajorBranchStatus != PublishStepPlanned {
		t.Fatalf("got status %q, want planned", meta.MajorBranchStatus)
	}
}

func TestPublishMajorBranchAdvancesWithObservedLeaseAndVerifies(t *testing.T) {
	head := "2222222222222222222222222222222222222222"
	observed := "1111111111111111111111111111111111111111"
	ref := "refs/heads/v3"
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "git", args: []string{"update-ref", ref, head}},
		{name: "git", args: []string{"push", "--force-with-lease=" + ref + ":" + observed, "upstream", ref + ":" + ref}},
		{name: "git", args: []string{"ls-remote", "--heads", "upstream", ref}, out: head + "\t" + ref + "\n"},
	}}
	meta := PublishMeta{Remote: "upstream", MajorBranch: "v3", HeadCommit: head, MajorBranchStatus: PublishStepPlanned}

	if err := publishMajorBranch(t.TempDir(), &meta, publishGitState{remoteMajorCommit: observed}, runner, false, true); err != nil {
		t.Fatalf("publishMajorBranch returned error: %v", err)
	}
	runner.done()
	if meta.MajorBranchStatus != PublishStepCompleted {
		t.Fatalf("got status %q, want completed", meta.MajorBranchStatus)
	}
}

func TestPublishMajorBranchReusesCurrentRemote(t *testing.T) {
	head := "3333333333333333333333333333333333333333"
	runner := &publishTestRunner{t: t}
	meta := PublishMeta{Remote: "origin", MajorBranch: "v1", HeadCommit: head, MajorBranchStatus: PublishStepReused}

	if err := publishMajorBranch(t.TempDir(), &meta, publishGitState{remoteMajorCommit: head}, runner, false, true); err != nil {
		t.Fatalf("publishMajorBranch returned error: %v", err)
	}
	runner.done()
	if meta.MajorBranchStatus != PublishStepReused {
		t.Fatalf("got status %q, want reused", meta.MajorBranchStatus)
	}
}

func TestPublishMajorBranchLeaseFailureIsResumable(t *testing.T) {
	head := "4444444444444444444444444444444444444444"
	observed := "3333333333333333333333333333333333333333"
	ref := "refs/heads/v4"
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "git", args: []string{"update-ref", ref, head}},
		{name: "git", args: []string{"push", "--force-with-lease=" + ref + ":" + observed, "origin", ref + ":" + ref}, out: "stale info\n", err: errors.New("exit 1")},
	}}
	meta := PublishMeta{Remote: "origin", MajorBranch: "v4", HeadCommit: head, MajorBranchStatus: PublishStepPlanned}

	err := publishMajorBranch(t.TempDir(), &meta, publishGitState{remoteMajorCommit: observed}, runner, false, true)
	if err == nil || !strings.Contains(err.Error(), "stale info") {
		t.Fatalf("expected lease failure, got %v", err)
	}
	runner.done()
	if meta.MajorBranchStatus != PublishStepPlanned {
		t.Fatalf("failed step changed status to %q", meta.MajorBranchStatus)
	}
}
