package integration

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
	buildCmd := exec.Command("go", "build", "-o", binPath, "../")
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
