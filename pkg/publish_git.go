package goversion

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type publishGitState struct {
	remoteURL          string
	remoteBranchCommit string
	remoteTagCommit    string
}

func inspectPublishGit(workDir, modPath string, meta *PublishMeta, runner publishCommandRunner) (publishGitState, error) {
	var state publishGitState

	if output, err := runner.Run(workDir, nil, "git", "status", "--porcelain"); err != nil {
		return state, publishCommandError("inspect git worktree", output, err, "git", "status", "--porcelain")
	} else if strings.TrimSpace(string(output)) != "" {
		return state, fmt.Errorf("git worktree is not clean; commit or stash changes before publishing:\n%s", strings.TrimSpace(string(output)))
	}

	rootOutput, err := runner.Run(workDir, nil, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return state, publishCommandError("find git repository root", rootOutput, err, "git", "rev-parse", "--show-toplevel")
	}
	meta.Tag, err = moduleVersionTag(strings.TrimSpace(string(rootOutput)), filepath.Dir(modPath), meta.Version)
	if err != nil {
		return state, err
	}

	branchOutput, err := runner.Run(workDir, nil, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return state, publishCommandError("determine current branch (detached HEAD is not publishable)", branchOutput, err, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
	}
	meta.Branch = strings.TrimSpace(string(branchOutput))
	if meta.Branch == "" {
		return state, errors.New("current git branch is empty")
	}

	headOutput, err := runner.Run(workDir, nil, "git", "rev-parse", "--verify", "HEAD")
	if err != nil {
		return state, publishCommandError("resolve HEAD", headOutput, err, "git", "rev-parse", "--verify", "HEAD")
	}
	meta.HeadCommit = strings.TrimSpace(string(headOutput))

	localTagRef := "refs/tags/" + meta.Tag + "^{commit}"
	localTagOutput, err := runner.Run(workDir, nil, "git", "rev-parse", "--verify", localTagRef)
	if err != nil {
		return state, publishCommandError("resolve local tag "+meta.Tag, localTagOutput, err, "git", "rev-parse", "--verify", localTagRef)
	}
	if localTagCommit := strings.TrimSpace(string(localTagOutput)); localTagCommit != meta.HeadCommit {
		return state, fmt.Errorf("local tag %s resolves to %s, not HEAD %s", meta.Tag, localTagCommit, meta.HeadCommit)
	}

	remoteOutput, err := runner.Run(workDir, nil, "git", "remote", "get-url", meta.Remote)
	if err != nil {
		return state, publishCommandError("find git remote "+meta.Remote, remoteOutput, err, "git", "remote", "get-url", meta.Remote)
	}
	state.remoteURL = strings.TrimSpace(string(remoteOutput))
	if state.remoteURL == "" {
		return state, fmt.Errorf("git remote %s has an empty URL", meta.Remote)
	}

	state.remoteTagCommit, err = inspectRemoteTag(workDir, meta.Remote, meta.Tag, runner)
	if err != nil {
		return state, err
	}
	if state.remoteTagCommit != "" && state.remoteTagCommit != meta.HeadCommit {
		return state, fmt.Errorf("remote tag %s on %s resolves to %s, not HEAD %s", meta.Tag, meta.Remote, state.remoteTagCommit, meta.HeadCommit)
	}
	state.remoteBranchCommit, err = inspectRemoteBranch(workDir, meta.Remote, meta.Branch, runner)
	if err != nil {
		return state, err
	}

	meta.TagStatus = statusForRemoteCommit(state.remoteTagCommit, meta.HeadCommit)
	meta.BranchStatus = statusForRemoteCommit(state.remoteBranchCommit, meta.HeadCommit)
	return state, nil
}

func statusForRemoteCommit(remoteCommit, headCommit string) PublishStepStatus {
	if remoteCommit == headCommit {
		return PublishStepReused
	}
	return PublishStepPlanned
}

func validatePublishGitRefs(workDir string, meta *PublishMeta, state publishGitState, runner publishCommandRunner) error {
	refspecs := publishGitRefspecs(meta, state)
	if len(refspecs) == 0 {
		return nil
	}
	args := append([]string{"push", "--dry-run", "--atomic", meta.Remote}, refspecs...)
	output, err := runner.Run(workDir, nil, "git", args...)
	if err != nil {
		return publishCommandError("validate Git ref publication", output, err, "git", args...)
	}
	return nil
}

func publishGitRefs(workDir string, meta *PublishMeta, state publishGitState, runner publishCommandRunner) error {
	refspecs := publishGitRefspecs(meta, state)
	if len(refspecs) != 0 {
		pushArgs := append([]string{"push", "--atomic", meta.Remote}, refspecs...)
		pushOutput, err := runner.Run(workDir, nil, "git", pushArgs...)
		if err != nil {
			return publishCommandError("atomically publish Git refs", pushOutput, err, "git", pushArgs...)
		}
		if state.remoteBranchCommit != meta.HeadCommit {
			meta.BranchStatus = PublishStepCompleted
		}
		if state.remoteTagCommit == "" {
			meta.TagStatus = PublishStepCompleted
		}
	}

	remoteTagCommit, err := inspectRemoteTag(workDir, meta.Remote, meta.Tag, runner)
	if err != nil {
		return err
	}
	if remoteTagCommit != meta.HeadCommit {
		return fmt.Errorf("remote tag %s on %s resolves to %q after push, expected HEAD %s", meta.Tag, meta.Remote, remoteTagCommit, meta.HeadCommit)
	}
	remoteBranchCommit, err := inspectRemoteBranch(workDir, meta.Remote, meta.Branch, runner)
	if err != nil {
		return err
	}
	if remoteBranchCommit != meta.HeadCommit {
		return fmt.Errorf("remote branch %s on %s resolves to %q after push, expected HEAD %s", meta.Branch, meta.Remote, remoteBranchCommit, meta.HeadCommit)
	}
	return nil
}

func publishGitRefspecs(meta *PublishMeta, state publishGitState) []string {
	var refspecs []string
	if state.remoteBranchCommit != meta.HeadCommit {
		refspecs = append(refspecs, "HEAD:refs/heads/"+meta.Branch)
	}
	if state.remoteTagCommit == "" {
		refspecs = append(refspecs, "refs/tags/"+meta.Tag+":refs/tags/"+meta.Tag)
	}
	return refspecs
}

func inspectRemoteBranch(workDir, remote, branch string, runner publishCommandRunner) (string, error) {
	args := []string{"ls-remote", "--heads", remote, "refs/heads/" + branch}
	output, err := runner.Run(workDir, nil, "git", args...)
	if err != nil {
		return "", publishCommandError("inspect remote branch "+branch, output, err, "git", args...)
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return "", nil
	}
	if len(fields) != 2 || fields[1] != "refs/heads/"+branch {
		return "", fmt.Errorf("unexpected git ls-remote output for branch %s: %s", branch, strings.TrimSpace(string(output)))
	}
	return fields[0], nil
}

func inspectRemoteTag(workDir, remote, tag string, runner publishCommandRunner) (string, error) {
	args := []string{"ls-remote", "--tags", remote, "refs/tags/" + tag, "refs/tags/" + tag + "^{}"}
	output, err := runner.Run(workDir, nil, "git", args...)
	if err != nil {
		return "", publishCommandError("inspect remote tag "+tag, output, err, "git", args...)
	}

	var exact, peeled string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[1] {
		case "refs/tags/" + tag:
			exact = fields[0]
		case "refs/tags/" + tag + "^{}":
			peeled = fields[0]
		}
	}
	if peeled != "" {
		return peeled, nil
	}
	return exact, nil
}
