package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIPublishHelp(t *testing.T) {
	out, err := runCLI([]string{"publish", "-help"})
	if err != nil {
		t.Fatalf("publish help failed: %v\n%s", err, out)
	}
	for _, want := range []string{"goversion publish [options]", "-timeout duration", "-major-branch"} {
		if !strings.Contains(out, want) {
			t.Errorf("publish help does not contain %q:\n%s", want, out)
		}
	}
}

func TestCLIPublishToLocalRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	base := t.TempDir()
	remoteDir := filepath.Join(base, "remote.git")
	workDir := filepath.Join(base, "work")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, output)
		}
		return strings.TrimSpace(string(output))
	}

	run(base, "git", "init", "--bare", remoteDir)
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(workDir, "git", "init")
	run(workDir, "git", "branch", "-M", "master")
	run(workDir, "git", "config", "user.email", "test@example.com")
	run(workDir, "git", "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module example.com/acme/tool\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "version.go"), []byte("package tool\n\nvar Version = \"1.2.3\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(workDir, "git", "add", ".")
	run(workDir, "git", "commit", "-m", "1.2.3")
	run(workDir, "git", "tag", "v1.2.3")
	run(workDir, "git", "remote", "add", "origin", remoteDir)

	cmd := exec.Command(os.Args[0], "publish", "-no-release", "-no-proxy", "-major-branch")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "GO_HELPER_PROCESS=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("publish failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Go module published successfully!") || !strings.Contains(string(output), "==> Validating module and version...") {
		t.Fatalf("unexpected publish output:\n%s", output)
	}

	retry := exec.Command(os.Args[0], "publish", "-no-release", "-no-proxy", "-major-branch")
	retry.Dir = workDir
	retry.Env = append(os.Environ(), "GO_HELPER_PROCESS=1")
	retryOutput, err := retry.CombinedOutput()
	if err != nil {
		t.Fatalf("publish retry failed: %v\n%s", err, retryOutput)
	}
	if !strings.Contains(string(retryOutput), "Branch:  already current") || !strings.Contains(string(retryOutput), "Tag:     already current") || !strings.Contains(string(retryOutput), "Major:   v1 already current") {
		t.Fatalf("publish retry did not reuse completed Git refs:\n%s", retryOutput)
	}

	head := run(workDir, "git", "rev-parse", "HEAD")
	remoteTag := run(workDir, "git", "--git-dir", remoteDir, "rev-parse", "refs/tags/v1.2.3^{commit}")
	remoteBranch := run(workDir, "git", "--git-dir", remoteDir, "rev-parse", "refs/heads/master")
	remoteMajor := run(workDir, "git", "--git-dir", remoteDir, "rev-parse", "refs/heads/v1")
	if remoteTag != head || remoteBranch != head || remoteMajor != head {
		t.Fatalf("published refs do not match HEAD %s: tag=%s branch=%s major=%s", head, remoteTag, remoteBranch, remoteMajor)
	}

	if err := os.WriteFile(filepath.Join(workDir, "version.go"), []byte("package tool\n\nvar Version = \"1.2.4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(workDir, "git", "add", "version.go")
	run(workDir, "git", "commit", "-m", "1.2.4")
	run(workDir, "git", "tag", "v1.2.4")
	advancedHead := run(workDir, "git", "rev-parse", "HEAD")
	advance := exec.Command(os.Args[0], "publish", "-no-release", "-no-proxy", "-major-branch")
	advance.Dir = workDir
	advance.Env = append(os.Environ(), "GO_HELPER_PROCESS=1")
	if output, err := advance.CombinedOutput(); err != nil {
		t.Fatalf("publish advancement failed: %v\n%s", err, output)
	}
	advancedMajor := run(workDir, "git", "--git-dir", remoteDir, "rev-parse", "refs/heads/v1")
	if advancedMajor != advancedHead {
		t.Fatalf("moving major branch was not advanced: got %s, want %s", advancedMajor, advancedHead)
	}
}
