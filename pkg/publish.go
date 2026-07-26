package goversion

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

const (
	defaultPublishRemote  = "origin"
	defaultPublishProxy   = "https://proxy.golang.org"
	defaultPublishTimeout = 2 * time.Minute
)

// PublishStepStatus describes the state of one step in a publish operation.
type PublishStepStatus string

const (
	// PublishStepPending indicates that the step has not yet been evaluated.
	PublishStepPending PublishStepStatus = "pending"
	// PublishStepPlanned indicates that a dry run found an action would be performed.
	PublishStepPlanned PublishStepStatus = "planned"
	// PublishStepCompleted indicates that Publish performed the action successfully.
	PublishStepCompleted PublishStepStatus = "completed"
	// PublishStepReused indicates that the required remote state already existed.
	PublishStepReused PublishStepStatus = "reused"
	// PublishStepSkipped indicates that the step was disabled or unavailable.
	PublishStepSkipped PublishStepStatus = "skipped"
)

// PublishOptions configures a Go module publish operation.
type PublishOptions struct {
	// WorkDir is the directory from which to locate go.mod and run commands.
	// It defaults to the current directory.
	WorkDir string
	// VersionFile contains the version to publish and defaults to ./version.go within WorkDir.
	VersionFile string
	// Remote is the Git remote to publish to and defaults to origin.
	Remote string
	// Proxy is the Go module proxy to seed and defaults to https://proxy.golang.org.
	Proxy string
	// DryRun performs all local and remote preflight checks without publishing.
	DryRun bool
	// NoProxy skips Go module proxy seeding.
	NoProxy bool
	// NoRelease skips GitHub Release creation.
	NoRelease bool
	// Timeout limits each external git, gh, and go command.
	// It defaults to two minutes. A negative value disables the timeout.
	Timeout time.Duration
	// Progress receives status messages before each publish phase.
	// It may be nil to disable progress output.
	Progress io.Writer
}

// PublishMeta describes the module release and the state of each publish step.
type PublishMeta struct {
	// ModulePath is the module directive read from go.mod.
	ModulePath string
	// Version is the published semantic version with a leading v prefix.
	Version string
	// Tag is the local and remote Git tag for Version.
	Tag string
	// HeadCommit is the commit identified by Tag.
	HeadCommit string
	// Branch is the current local branch published to Remote.
	Branch string
	// Remote is the Git remote used for publication.
	Remote string
	// BranchStatus describes whether the remote branch was planned, completed, or reused.
	BranchStatus PublishStepStatus
	// TagStatus describes whether the remote tag was planned, completed, or reused.
	TagStatus PublishStepStatus
	// ReleaseStatus describes whether the GitHub Release was planned, completed, reused, or skipped.
	ReleaseStatus PublishStepStatus
	// ProxyStatus describes whether proxy seeding was planned, completed, or skipped.
	ProxyStatus PublishStepStatus
	// ReleaseURL is the URL of the created or reused GitHub Release.
	ReleaseURL string
	// Warnings contains non-fatal conditions encountered while publishing.
	Warnings []string
}

type publishCommandRunner interface {
	Run(dir string, env []string, name string, args ...string) ([]byte, error)
	LookPath(name string) (string, error)
	TempDir(pattern string) (string, error)
	Sleep(duration time.Duration) error
}

type execPublishCommandRunner struct {
	ctx     context.Context
	timeout time.Duration
}

func (runner execPublishCommandRunner) Run(dir string, env []string, name string, args ...string) ([]byte, error) {
	ctx := runner.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	cancel := func() {}
	if runner.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, runner.timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if ctxErr := ctx.Err(); ctxErr != nil {
		if runner.timeout > 0 && ctxErr == context.DeadlineExceeded {
			return output, fmt.Errorf("command timed out after %s: %w", runner.timeout, ctxErr)
		}
		return output, ctxErr
	}
	return output, err
}

func (execPublishCommandRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (execPublishCommandRunner) TempDir(pattern string) (string, error) {
	return os.MkdirTemp("", pattern)
}

func (runner execPublishCommandRunner) Sleep(duration time.Duration) error {
	ctx := runner.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Publish publishes an existing version commit and tag as a Go module release.
//
// Publish validates the repository-root module, clean worktree, current branch,
// and local version tag before changing remote state. It pushes only incomplete
// Git refs, creates or reuses a GitHub Release through gh, and seeds the
// configured Go module proxy. Missing or unauthenticated gh support produces a
// warning and skips the GitHub Release. Retrying Publish after a failure reuses
// completed Git refs and an existing GitHub Release before continuing.
func Publish(options PublishOptions) (PublishMeta, error) {
	return PublishContext(context.Background(), options)
}

// PublishContext behaves like Publish and cancels active external commands when ctx is done.
func PublishContext(ctx context.Context, options PublishOptions) (PublishMeta, error) {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = defaultPublishTimeout
	}
	return publish(options, execPublishCommandRunner{ctx: ctx, timeout: timeout})
}

func publish(options PublishOptions, runner publishCommandRunner) (PublishMeta, error) {
	meta := PublishMeta{
		BranchStatus:  PublishStepPending,
		TagStatus:     PublishStepPending,
		ReleaseStatus: PublishStepPending,
		ProxyStatus:   PublishStepPending,
	}

	publishProgress(options.Progress, "Validating module and version")

	workDir := options.WorkDir
	if workDir == "" {
		workDir = "."
	}
	workDir, err := filepath.Abs(workDir)
	if err != nil {
		return meta, fmt.Errorf("resolve work directory: %w", err)
	}
	if info, err := os.Stat(workDir); err != nil {
		return meta, fmt.Errorf("stat work directory %q: %w", workDir, err)
	} else if !info.IsDir() {
		return meta, fmt.Errorf("work directory %q is not a directory", workDir)
	}

	remote := options.Remote
	if remote == "" {
		remote = defaultPublishRemote
	}
	proxy := options.Proxy
	if proxy == "" {
		proxy = defaultPublishProxy
	}
	meta.Remote = remote

	modPath, modulePath, err := findAndParseGoMod(workDir)
	if err != nil {
		return meta, err
	}
	meta.ModulePath = modulePath

	versionFile := options.VersionFile
	if versionFile == "" {
		versionFile = "./version.go"
	}
	if !filepath.IsAbs(versionFile) {
		versionFile = filepath.Join(workDir, versionFile)
	}
	version, err := readPublishVersion(versionFile)
	if err != nil {
		return meta, err
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if !semver.IsValid(version) {
		return meta, fmt.Errorf("version %q from %q is not valid semantic versioning", version, versionFile)
	}
	if err := validateModuleMajor(modulePath, version); err != nil {
		return meta, err
	}
	meta.Version = version

	publishProgress(options.Progress, "Inspecting local and remote Git state")
	gitState, err := inspectPublishGit(workDir, modPath, &meta, runner)
	if err != nil {
		return meta, err
	}

	if options.DryRun {
		publishProgress(options.Progress, "Validating Git ref publication (dry run)")
		if err := validatePublishGitRefs(workDir, &meta, gitState, runner); err != nil {
			return meta, err
		}
		if options.NoRelease {
			publishProgress(options.Progress, "Skipping GitHub Release")
		} else {
			publishProgress(options.Progress, "Checking GitHub Release")
		}
		if err := publishGitHubRelease(workDir, gitState.remoteURL, &meta, runner, options.NoRelease, true); err != nil {
			return meta, err
		}
		planPublishProxy(&meta, options.NoProxy)
		return meta, nil
	}

	publishProgress(options.Progress, "Publishing Git refs")
	if err := publishGitRefs(workDir, &meta, gitState, runner); err != nil {
		return meta, err
	}
	if options.NoRelease {
		publishProgress(options.Progress, "Skipping GitHub Release")
	} else {
		publishProgress(options.Progress, "Creating or reusing GitHub Release")
	}
	if err := publishGitHubRelease(workDir, gitState.remoteURL, &meta, runner, options.NoRelease, false); err != nil {
		return meta, err
	}
	if options.NoProxy {
		publishProgress(options.Progress, "Skipping Go module proxy")
	} else {
		publishProgress(options.Progress, "Seeding Go module proxy")
	}
	if err := seedPublishProxy(filepath.Dir(modPath), proxy, &meta, runner, options.Progress, options.NoProxy); err != nil {
		return meta, err
	}

	return meta, nil
}

func publishProgress(output io.Writer, message string) {
	if output != nil {
		fmt.Fprintf(output, "==> %s...\n", message)
	}
}

func moduleVersionTag(gitRoot, moduleDir, version string) (string, error) {
	relative, err := filepath.Rel(gitRoot, moduleDir)
	if err != nil {
		return "", fmt.Errorf("resolve module directory relative to git root: %w", err)
	}
	if relative != "." {
		return "", fmt.Errorf("publishing nested Go module %q is not supported; run goversion from a repository-root module", filepath.ToSlash(relative))
	}
	return version, nil
}

func findAndParseGoMod(start string) (string, string, error) {
	dir := start
	for {
		path := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(path)
		if err == nil {
			file, err := modfile.Parse(path, data, nil)
			if err != nil {
				return "", "", fmt.Errorf("parse %q: %w", path, err)
			}
			if file.Module == nil || file.Module.Mod.Path == "" {
				return "", "", fmt.Errorf("go.mod %q has no module directive", path)
			}
			if err := module.CheckPath(file.Module.Mod.Path); err != nil {
				return "", "", fmt.Errorf("invalid module path %q: %w", file.Module.Mod.Path, err)
			}
			return path, file.Module.Mod.Path, nil
		}
		if !os.IsNotExist(err) {
			return "", "", fmt.Errorf("read %q: %w", path, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("no go.mod found from %q or its parent directories", start)
		}
		dir = parent
	}
}

func readPublishVersion(path string) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return "", fmt.Errorf("parse version file %q: %w", path, err)
	}
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok || (generic.Tok != token.VAR && generic.Tok != token.CONST) {
			continue
		}
		for _, specification := range generic.Specs {
			valueSpec, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range valueSpec.Names {
				if name.Name != "Version" || index >= len(valueSpec.Values) {
					continue
				}
				literal, ok := valueSpec.Values[index].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return "", fmt.Errorf("Version in %q must be a string literal", path)
				}
				version, err := strconv.Unquote(literal.Value)
				if err != nil {
					return "", fmt.Errorf("parse Version in %q: %w", path, err)
				}
				return version, nil
			}
		}
	}
	return "", fmt.Errorf("Version string declaration not found in %q", path)
}

func validateModuleMajor(modulePath, version string) error {
	_, pathMajor, ok := module.SplitPathVersion(modulePath)
	if !ok {
		return fmt.Errorf("module path %q has an invalid major version suffix", modulePath)
	}
	if err := module.CheckPathMajor(version, pathMajor); err != nil {
		return fmt.Errorf("version %s does not match module path %q: %w", version, modulePath, err)
	}
	return nil
}

func publishCommandError(action string, output []byte, err error, name string, args ...string) error {
	command := strings.Join(append([]string{name}, args...), " ")
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %s failed: %w", action, command, err)
	}
	return fmt.Errorf("%s: %s failed: %w; output: %s", action, command, err, detail)
}
