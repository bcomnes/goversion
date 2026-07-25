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
	if !strings.Contains(out, "goversion publish [options]") {
		t.Errorf("expected publish help output, got:\n%s", out)
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

	cmd := exec.Command(os.Args[0], "publish", "-no-release", "-no-proxy")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "GO_HELPER_PROCESS=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("publish failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Go module published successfully!") {
		t.Fatalf("unexpected publish output:\n%s", output)
	}

	retry := exec.Command(os.Args[0], "publish", "-no-release", "-no-proxy")
	retry.Dir = workDir
	retry.Env = append(os.Environ(), "GO_HELPER_PROCESS=1")
	retryOutput, err := retry.CombinedOutput()
	if err != nil {
		t.Fatalf("publish retry failed: %v\n%s", err, retryOutput)
	}
	if !strings.Contains(string(retryOutput), "Branch:  already current") || !strings.Contains(string(retryOutput), "Tag:     already current") {
		t.Fatalf("publish retry did not reuse completed Git refs:\n%s", retryOutput)
	}

	head := run(workDir, "git", "rev-parse", "HEAD")
	remoteTag := run(workDir, "git", "--git-dir", remoteDir, "rev-parse", "refs/tags/v1.2.3^{commit}")
	remoteBranch := run(workDir, "git", "--git-dir", remoteDir, "rev-parse", "refs/heads/master")
	if remoteTag != head || remoteBranch != head {
		t.Fatalf("published refs do not match HEAD %s: tag=%s branch=%s", head, remoteTag, remoteBranch)
	}
}
