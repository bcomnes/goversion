package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCLIBinaryIntegration(t *testing.T) {
	// 1. Build the CLI binary.
	// Create a temporary directory for the build.
	tmpBuildDir, err := os.MkdirTemp("", "goversion_build")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpBuildDir)

	// The built binary will be written to "goversion" in tmpBuildDir.
	binPath := filepath.Join(tmpBuildDir, "goversion")
	// Build the CLI binary from the main package.
	// Since this test resides in cmd/integration, the main package is in its parent directory ("../").
	buildCmd := exec.Command("go", "build", "-o", binPath, "./")
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build CLI binary: %v; build output: %s", err, string(buildOutput))
	}

	// 2. Set up a temporary git repository for testing.
	tmpRepo, err := os.MkdirTemp("", "goversion_integration")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpRepo)

	// Initialize a new git repository.
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tmpRepo
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v; output: %s", err, string(output))
	}

	// Configure git user.
	configCmds := [][]string{
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test User"},
	}
	for _, args := range configCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpRepo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config failed: %v; output: %s", err, string(output))
		}
	}

	// 3. Create the version file in a pkg directory.
	pkgDir := filepath.Join(tmpRepo, "pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create pkg directory: %v", err)
	}
	versionFilePath := filepath.Join(pkgDir, "version.go")
	initialVersionContent := `package version

var (
	Version = "1.2.3"
)
`
	if err := os.WriteFile(versionFilePath, []byte(initialVersionContent), 0644); err != nil {
		t.Fatalf("failed to write version file: %v", err)
	}

	// 4. Stage and commit the initial version file.
	gitAddCmd := exec.Command("git", "add", ".")
	gitAddCmd.Dir = tmpRepo
	if output, err := gitAddCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v; output: %s", err, string(output))
	}
	gitCommitCmd := exec.Command("git", "commit", "-m", "initial commit")
	gitCommitCmd.Dir = tmpRepo
	if output, err := gitCommitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v; output: %s", err, string(output))
	}

	// 5. Run the CLI binary.
	// Use the -version-file flag to point to our version file and use "patch" as the positional argument.
	cliCmd := exec.Command(binPath, "-version-file", versionFilePath, "patch")
	cliCmd.Dir = tmpRepo
	var cliStdout, cliStderr bytes.Buffer
	cliCmd.Stdout = &cliStdout
	cliCmd.Stderr = &cliStderr
	if err := cliCmd.Run(); err != nil {
		t.Fatalf("CLI command failed: %v; stdout: %s; stderr: %s", err, cliStdout.String(), cliStderr.String())
	}

	// 6. Verify the version file was updated to "1.2.4".
	updatedContent, err := os.ReadFile(versionFilePath)
	if err != nil {
		t.Fatalf("failed to read version file: %v", err)
	}
	if !strings.Contains(string(updatedContent), `Version = "1.2.4"`) {
		t.Errorf("version file not updated; expected 'Version = \"1.2.4\"' in content, got:\n%s", string(updatedContent))
	}

	// 7. Verify that a git tag "v1.2.4" was created.
	gitTagCmd := exec.Command("git", "tag")
	gitTagCmd.Dir = tmpRepo
	tagOutput, err := gitTagCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag command failed: %v; output: %s", err, string(tagOutput))
	}
	tags := strings.Split(strings.TrimSpace(string(tagOutput)), "\n")
	expectedTag := "v1.2.4"
	if !slices.Contains(tags, expectedTag) {
		t.Errorf("expected git tag %q not found; got tags: %v", expectedTag, tags)
	}
}

// TestCLIBinaryMajorBumpIntegration builds the goversion CLI and then
// runs a major bump against a temp repo with both version.go and go.mod,
// asserting that version.go and go.mod are updated and that a v2.0.0 tag is created.
func TestCLIBinaryMajorBumpIntegration(t *testing.T) {
	// 1. Build the CLI binary into a temp directory.
	tmpBuildDir, err := os.MkdirTemp("", "goversion_build_major")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpBuildDir)

	binPath := filepath.Join(tmpBuildDir, "goversion")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build CLI binary: %v; output: %s", err, out)
	}

	// 2. Initialize a fresh git repo for the test.
	tmpRepo, err := os.MkdirTemp("", "goversion_major_integration")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpRepo)

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpRepo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")

	// 3. Create go.mod at the repo root.
	modContent := `module example.com/m

go 1.18
`
	if err := os.WriteFile(filepath.Join(tmpRepo, "go.mod"), []byte(modContent), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// 4. Create version.go in pkg/.
	pkgDir := filepath.Join(tmpRepo, "pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to mkdir pkg: %v", err)
	}
	relVer := filepath.Join("pkg", "version.go")
	absVer := filepath.Join(tmpRepo, relVer)
	initial := `package version

var (
	Version = "1.2.3"
)
`
	if err := os.WriteFile(absVer, []byte(initial), 0644); err != nil {
		t.Fatalf("failed to write version.go: %v", err)
	}

	// 5. Commit both files.
	runGit("add", ".")
	runGit("commit", "-m", "initial commit")

	// 6. Run the CLI binary with "major".
	cmd := exec.Command(binPath, "-version-file", relVer, "major")
	cmd.Dir = tmpRepo
	cmd.Env = append(os.Environ(),
		"GO_HELPER_PROCESS=1",
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("CLI major bump failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	// 7. Verify version.go updated to 2.0.0.
	updatedVer, err := os.ReadFile(absVer)
	if err != nil {
		t.Fatalf("reading version.go failed: %v", err)
	}
	if !strings.Contains(string(updatedVer), `Version = "2.0.0"`) {
		t.Errorf("version.go not updated; got:\n%s", updatedVer)
	}

	// 8. Verify go.mod module line now includes /v2.
	modGot, err := os.ReadFile(filepath.Join(tmpRepo, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod failed: %v", err)
	}
	first := strings.SplitN(string(modGot), "\n", 2)[0]
	if !strings.Contains(first, "/v2") {
		t.Errorf("go.mod module line = %q; want it to include \"/v2\"", first)
	}

	// 9. Verify git tag "v2.0.0" exists in this repo.
	tagCmd := exec.Command("git", "tag")
	tagCmd.Dir = tmpRepo
	tagOutput, err := tagCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag failed: %v; output: %s", err, tagOutput)
	}
	tags := strings.Split(strings.TrimSpace(string(tagOutput)), "\n")
	if !slices.Contains(tags, "v2.0.0") {
		t.Errorf("expected git tag v2.0.0; got tags: %v", tags)
	}
}
