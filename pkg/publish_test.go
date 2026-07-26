package goversion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

type publishTestCommand struct {
	name string
	args []string
	env  []string
	out  string
	err  error
}

type publishTestRunner struct {
	t        *testing.T
	commands []publishTestCommand
	calls    []publishTestCommand
	lookErr  error
	sleeps   []time.Duration
	sleepErr error
}

func (runner *publishTestRunner) Run(_ string, env []string, name string, args ...string) ([]byte, error) {
	runner.t.Helper()
	call := publishTestCommand{name: name, args: append([]string(nil), args...), env: append([]string(nil), env...)}
	runner.calls = append(runner.calls, call)
	if len(runner.commands) == 0 {
		runner.t.Fatalf("unexpected command: %s %s", name, strings.Join(args, " "))
	}
	expected := runner.commands[0]
	runner.commands = runner.commands[1:]
	if name != expected.name || !reflect.DeepEqual(args, expected.args) || !publishTestEnvironmentsEqual(env, expected.env) {
		runner.t.Fatalf("command mismatch\n got: %s %v env=%v\nwant: %s %v env=%v", name, args, env, expected.name, expected.args, expected.env)
	}
	return []byte(expected.out), expected.err
}

func (runner *publishTestRunner) RunCaptured(dir string, env []string, name string, args ...string) ([]byte, error) {
	return runner.Run(dir, env, name, args...)
}

func publishTestEnvironmentsEqual(actual, expected []string) bool {
	var filtered []string
	hasModuleCache := false
	for _, value := range actual {
		if strings.HasPrefix(value, "GOMODCACHE=") {
			hasModuleCache = strings.TrimPrefix(value, "GOMODCACHE=") != ""
			continue
		}
		filtered = append(filtered, value)
	}
	if len(actual) > len(expected) && !hasModuleCache {
		return false
	}
	return reflect.DeepEqual(filtered, expected)
}

func (runner *publishTestRunner) LookPath(name string) (string, error) {
	runner.t.Helper()
	if name != "gh" {
		runner.t.Fatalf("unexpected executable lookup: %s", name)
	}
	if runner.lookErr != nil {
		return "", runner.lookErr
	}
	return "/usr/local/bin/gh", nil
}

func (runner *publishTestRunner) TempDir(_ string) (string, error) {
	return runner.t.TempDir(), nil
}

func (runner *publishTestRunner) Sleep(duration time.Duration) error {
	runner.sleeps = append(runner.sleeps, duration)
	return runner.sleepErr
}

func (runner *publishTestRunner) done() {
	runner.t.Helper()
	if len(runner.commands) != 0 {
		runner.t.Fatalf("%d expected commands were not run; next is %s %v", len(runner.commands), runner.commands[0].name, runner.commands[0].args)
	}
}

func TestPublishSuccessStatuses(t *testing.T) {
	dir := writePublishFixture(t, "example.com/acme/tool/v2", "2.1.0-beta.1")
	head := "0123456789abcdef0123456789abcdef01234567"
	tag := "v2.1.0-beta.1"
	remoteURL := "git@github.com:acme/tool.git"
	runner := preflightRunnerWithRefs(t, dir, head, tag, remoteURL, "", "")
	runner.commands = append(runner.commands,
		publishTestCommand{name: "git", args: []string{"push", "--atomic", "origin", "HEAD:refs/heads/main", "refs/tags/" + tag + ":refs/tags/" + tag}},
		remoteTagCommand(tag, head),
		remoteBranchCommand(head),
		publishTestCommand{name: "gh", args: []string{"auth", "status", "--hostname", "github.com"}},
		publishTestCommand{name: "gh", args: []string{"repo", "view", remoteURL, "--json", "nameWithOwner", "--jq", ".nameWithOwner"}, out: "acme/tool\n"},
		publishTestCommand{name: "gh", args: []string{"release", "view", tag, "--repo", "github.com/acme/tool", "--json", "url", "--jq", ".url"}, out: "release not found\n", err: errors.New("not found")},
		publishTestCommand{name: "gh", args: []string{"release", "create", tag, "--verify-tag", "--generate-notes", "--title", tag, "--repo", "github.com/acme/tool", "--prerelease"}, out: "https://github.com/acme/tool/releases/tag/" + tag + "\n"},
		publishTestCommand{name: "go", args: []string{"mod", "download", "-json", "example.com/acme/tool/v2@" + tag}, env: []string{"GOWORK=off", "GOSUMDB=off", "GOPROXY=https://proxy.golang.org"}, out: "{\"Path\":\"example.com/acme/tool/v2\",\"Version\":\"" + tag + "\"}\n"},
	)

	meta, err := publish(PublishOptions{WorkDir: dir}, runner)
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	runner.done()
	if meta.ModulePath != "example.com/acme/tool/v2" || meta.Version != tag || meta.Tag != tag {
		t.Fatalf("unexpected module metadata: %+v", meta)
	}
	if meta.HeadCommit != head || meta.Branch != "main" || meta.Remote != "origin" {
		t.Fatalf("unexpected git metadata: %+v", meta)
	}
	assertPublishStatuses(t, meta, PublishStepCompleted, PublishStepCompleted, PublishStepCompleted, PublishStepCompleted)
	if meta.ReleaseURL != "https://github.com/acme/tool/releases/tag/"+tag {
		t.Fatalf("unexpected release URL: %q", meta.ReleaseURL)
	}
}

func TestPublishDryRunStatusesAndNoMutation(t *testing.T) {
	dir := writePublishFixture(t, "example.com/acme/tool", "1.2.3")
	before, err := os.ReadFile(filepath.Join(dir, "version.go"))
	if err != nil {
		t.Fatal(err)
	}
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tag := "v1.2.3"
	remoteURL := "git@example.com:acme/tool.git"
	runner := preflightRunnerWithRefs(t, dir, head, tag, remoteURL, "", head)
	runner.commands = append(runner.commands,
		publishTestCommand{name: "git", args: []string{"push", "--dry-run", "--atomic", "origin", "refs/tags/" + tag + ":refs/tags/" + tag}},
		publishTestCommand{name: "gh", args: []string{"auth", "status", "--hostname", "example.com"}},
		publishTestCommand{name: "gh", args: []string{"repo", "view", remoteURL, "--json", "nameWithOwner", "--jq", ".nameWithOwner"}, out: "acme/tool\n"},
		publishTestCommand{name: "gh", args: []string{"release", "view", tag, "--repo", "example.com/acme/tool", "--json", "url", "--jq", ".url"}, out: "release not found\n", err: errors.New("not found")},
	)

	meta, err := publish(PublishOptions{WorkDir: dir, DryRun: true}, runner)
	if err != nil {
		t.Fatalf("Publish dry run returned error: %v", err)
	}
	runner.done()
	assertPublishStatuses(t, meta, PublishStepReused, PublishStepPlanned, PublishStepPlanned, PublishStepPlanned)
	after, err := os.ReadFile(filepath.Join(dir, "version.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("dry run mutated version.go")
	}
}

func TestPublishDryRunDisabledSteps(t *testing.T) {
	dir := writePublishFixture(t, "example.com/acme/tool", "1.2.3")
	head := "abababababababababababababababababababab"
	tag := "v1.2.3"
	runner := preflightRunnerWithRefs(t, dir, head, tag, "git@example.com:acme/tool.git", head, head)

	var progress bytes.Buffer
	meta, err := publish(PublishOptions{WorkDir: dir, DryRun: true, NoRelease: true, NoProxy: true, Progress: &progress}, runner)
	if err != nil {
		t.Fatalf("Publish dry run returned error: %v", err)
	}
	runner.done()
	assertPublishStatuses(t, meta, PublishStepReused, PublishStepReused, PublishStepSkipped, PublishStepSkipped)
	for _, message := range []string{
		"Validating module and version",
		"Inspecting local and remote Git state",
		"Validating Git ref publication (dry run)",
		"Skipping GitHub Release",
	} {
		if !strings.Contains(progress.String(), message) {
			t.Errorf("progress output does not contain %q: %q", message, progress.String())
		}
	}
}

func TestExecPublishCommandRunnerStreamsOutput(t *testing.T) {
	if os.Getenv("GOVERSION_OUTPUT_TEST_HELPER") == "1" {
		fmt.Fprintln(os.Stdout, "child stdout")
		fmt.Fprintln(os.Stderr, "child stderr")
		return
	}

	var streamed bytes.Buffer
	runner := execPublishCommandRunner{
		ctx:     context.Background(),
		timeout: 10 * time.Second,
		output:  io.MultiWriter(&streamed, failingWriter{}),
	}
	output, err := runner.Run("", []string{"GOVERSION_OUTPUT_TEST_HELPER=1"}, os.Args[0], "-test.run=^TestExecPublishCommandRunnerStreamsOutput$")
	if err != nil {
		t.Fatalf("helper command failed: %v", err)
	}
	for _, want := range []string{"child stdout", "child stderr"} {
		if !strings.Contains(string(output), want) {
			t.Errorf("captured output does not contain %q: %q", want, output)
		}
		if !strings.Contains(streamed.String(), want) {
			t.Errorf("streamed output does not contain %q: %q", want, streamed.String())
		}
	}

	streamed.Reset()
	output, err = runner.RunCaptured("", []string{"GOVERSION_OUTPUT_TEST_HELPER=1"}, os.Args[0], "-test.run=^TestExecPublishCommandRunnerStreamsOutput$")
	if err != nil {
		t.Fatalf("captured helper command failed: %v", err)
	}
	if streamed.Len() != 0 || !strings.Contains(string(output), "child stdout") {
		t.Fatalf("captured command output was streamed=%q or not returned=%q", streamed.String(), output)
	}
}

func TestExecPublishCommandRunnerTimeout(t *testing.T) {
	if os.Getenv("GOVERSION_TIMEOUT_TEST_HELPER") == "1" {
		time.Sleep(5 * time.Second)
		return
	}

	runner := execPublishCommandRunner{ctx: context.Background(), timeout: 20 * time.Millisecond}
	started := time.Now()
	_, err := runner.Run("", []string{"GOVERSION_TIMEOUT_TEST_HELPER=1"}, os.Args[0], "-test.run=^TestExecPublishCommandRunnerTimeout$")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("timed-out command took %s to return", elapsed)
	}
	if !strings.Contains(err.Error(), "command timed out after 20ms") {
		t.Fatalf("timeout error is not actionable: %v", err)
	}
}

func TestExecPublishCommandRunnerBoundsInheritedOutput(t *testing.T) {
	if os.Getenv("GOVERSION_WAIT_TEST_GRANDCHILD") == "1" {
		for {
			if _, err := os.Stdout.Write([]byte(".")); err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if os.Getenv("GOVERSION_WAIT_TEST_CHILD") == "1" {
		child := exec.Command(os.Args[0], "-test.run=^TestExecPublishCommandRunnerBoundsInheritedOutput$")
		child.Env = append(os.Environ(), "GOVERSION_WAIT_TEST_GRANDCHILD=1")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		return
	}

	runner := execPublishCommandRunner{ctx: context.Background(), timeout: 10 * time.Second}
	started := time.Now()
	_, err := runner.Run("", []string{"GOVERSION_WAIT_TEST_CHILD=1"}, os.Args[0], "-test.run=^TestExecPublishCommandRunnerBoundsInheritedOutput$")
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("expected bounded inherited output error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("inherited output took %s to close", elapsed)
	}
}

func TestExecPublishCommandRunnerPreservesParentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	runner := execPublishCommandRunner{ctx: ctx, timeout: time.Minute}

	_, err := runner.Run("", []string{"GOVERSION_TIMEOUT_TEST_HELPER=1"}, os.Args[0], "-test.run=^TestExecPublishCommandRunnerTimeout$")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected parent deadline, got %v", err)
	}
	if strings.Contains(err.Error(), "command timed out after") {
		t.Fatalf("parent deadline was mislabeled as command timeout: %v", err)
	}
}

func TestPublishRejectsModuleMajorMismatch(t *testing.T) {
	dir := writePublishFixture(t, "example.com/acme/tool", "2.0.0")
	runner := &publishTestRunner{t: t}

	_, err := publish(PublishOptions{WorkDir: dir}, runner)
	if err == nil || !strings.Contains(err.Error(), "does not match module path") {
		t.Fatalf("expected module major mismatch, got %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("ran commands before rejecting module major mismatch: %v", runner.calls)
	}
}

func TestPublishRejectsDirtyTree(t *testing.T) {
	dir := writePublishFixture(t, "example.com/acme/tool", "1.2.3")
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "git", args: []string{"status", "--porcelain"}, out: " M version.go\n"},
	}}

	_, err := publish(PublishOptions{WorkDir: dir}, runner)
	if err == nil || !strings.Contains(err.Error(), "worktree is not clean") {
		t.Fatalf("expected dirty worktree error, got %v", err)
	}
	runner.done()
}

func TestPublishRejectsLocalTagMismatch(t *testing.T) {
	dir := writePublishFixture(t, "example.com/acme/tool", "1.2.3")
	head := "cccccccccccccccccccccccccccccccccccccccc"
	other := "dddddddddddddddddddddddddddddddddddddddd"
	tag := "v1.2.3"
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "git", args: []string{"status", "--porcelain"}},
		{name: "git", args: []string{"rev-parse", "--show-toplevel"}, out: dir + "\n"},
		{name: "git", args: []string{"symbolic-ref", "--quiet", "--short", "HEAD"}, out: "main\n"},
		{name: "git", args: []string{"rev-parse", "--verify", "HEAD"}, out: head + "\n"},
		{name: "git", args: []string{"rev-parse", "--verify", "refs/tags/" + tag + "^{commit}"}, out: other + "\n"},
	}}

	_, err := publish(PublishOptions{WorkDir: dir}, runner)
	if err == nil || !strings.Contains(err.Error(), "not HEAD") {
		t.Fatalf("expected local tag mismatch, got %v", err)
	}
	runner.done()
}

func TestModuleVersionTagRejectsNestedModule(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "tools", "widget")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := moduleVersionTag(root, moduleDir, "v1.2.3")
	if err == nil || !strings.Contains(err.Error(), "nested Go module") {
		t.Fatalf("expected nested module error, got %v", err)
	}
}

func assertPublishStatuses(t *testing.T, meta PublishMeta, branch, tag, release, proxy PublishStepStatus) {
	t.Helper()
	if meta.BranchStatus != branch || meta.TagStatus != tag || meta.ReleaseStatus != release || meta.ProxyStatus != proxy {
		t.Fatalf("unexpected statuses: branch=%q tag=%q release=%q proxy=%q; metadata: %+v", meta.BranchStatus, meta.TagStatus, meta.ReleaseStatus, meta.ProxyStatus, meta)
	}
}

func writePublishFixture(t *testing.T, modulePath, version string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(fmt.Sprintf("module %s\n\ngo 1.25\n", modulePath)), 0o644); err != nil {
		t.Fatal(err)
	}
	contents := fmt.Sprintf("package fixture\n\nvar Version = %q\n", version)
	if err := os.WriteFile(filepath.Join(dir, "version.go"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func preflightRunnerWithRefs(t *testing.T, dir, head, tag, remoteURL, remoteTagCommit, remoteBranchCommit string) *publishTestRunner {
	t.Helper()
	return &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "git", args: []string{"status", "--porcelain"}},
		{name: "git", args: []string{"rev-parse", "--show-toplevel"}, out: dir + "\n"},
		{name: "git", args: []string{"symbolic-ref", "--quiet", "--short", "HEAD"}, out: "main\n"},
		{name: "git", args: []string{"rev-parse", "--verify", "HEAD"}, out: head + "\n"},
		{name: "git", args: []string{"rev-parse", "--verify", "refs/tags/" + tag + "^{commit}"}, out: head + "\n"},
		{name: "git", args: []string{"remote", "get-url", "origin"}, out: remoteURL + "\n"},
		remoteTagCommand(tag, remoteTagCommit),
		remoteBranchCommand(remoteBranchCommit),
	}}
}

func remoteTagCommand(tag, commit string) publishTestCommand {
	output := ""
	if commit != "" {
		output = commit + "\trefs/tags/" + tag + "\n"
	}
	return publishTestCommand{name: "git", args: []string{"ls-remote", "--tags", "origin", "refs/tags/" + tag, "refs/tags/" + tag + "^{}"}, out: output}
}

func remoteBranchCommand(commit string) publishTestCommand {
	output := ""
	if commit != "" {
		output = commit + "\trefs/heads/main\n"
	}
	return publishTestCommand{name: "git", args: []string{"ls-remote", "--heads", "origin", "refs/heads/main"}, out: output}
}
