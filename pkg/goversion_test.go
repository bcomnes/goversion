package goversion

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestNormalizeVersion validates that normalizeVersion produces the expected output.
func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"dev", "v0.0.0"},
		{"1.2.3", "v1.2.3"},
		{"v1.2.3", "v1.2.3"},
	}
	for _, tc := range tests {
		res := normalizeVersion(tc.input)
		if res != tc.expected {
			t.Errorf("normalizeVersion(%q) = %q, expected %q", tc.input, res, tc.expected)
		}
	}
}

// TestParseAndFormatSemVer tests the parseSemVer and formatSemVer functions.
func TestParseAndFormatSemVer(t *testing.T) {
	tests := []struct {
		input                              string
		expectedMajor, expectedMinor, expectedPatch int
		expectedPrerelease                 string
	}{
		{"v1.2.3", 1, 2, 3, ""},
		{"v1.2.3-rc1", 1, 2, 3, "rc1"},
	}
	for _, tc := range tests {
		major, minor, patch, prerelease, err := parseSemVer(tc.input)
		if err != nil {
			t.Errorf("parseSemVer(%q) returned error: %v", tc.input, err)
			continue
		}
		if major != tc.expectedMajor || minor != tc.expectedMinor || patch != tc.expectedPatch || prerelease != tc.expectedPrerelease {
			t.Errorf("parseSemVer(%q) = (%d, %d, %d, %q), expected (%d, %d, %d, %q)",
				tc.input, major, minor, patch, prerelease,
				tc.expectedMajor, tc.expectedMinor, tc.expectedPatch, tc.expectedPrerelease)
		}
		reconstructed := formatSemVer(major, minor, patch, prerelease)
		if reconstructed != tc.input {
			t.Errorf("formatSemVer(%d, %d, %d, %q) = %q, expected %q", major, minor, patch, prerelease, reconstructed, tc.input)
		}
	}
}

// TestBumpVersion tests bumpVersion for various bump types.
func TestBumpVersion(t *testing.T) {
	tests := []struct {
		version  string // normalized version; must include "v"
		bump     string
		expected string // expected result with "v" prefix
	}{
		{"v1.2.3", "major", "v2.0.0"},
		{"v1.2.3", "minor", "v1.3.0"},
		{"v1.2.3", "patch", "v1.2.4"},
		{"v1.2.3", "premajor", "v2.0.0-0"},
		{"v1.2.3", "preminor", "v1.3.0-0"},
		{"v1.2.3", "prepatch", "v1.2.4-0"},
		{"v1.2.3", "prerelease", "v1.2.4-0"}, // no prerelease exists so bump patch and attach prerelease "0"
		{"v1.2.3-0", "prerelease", "v1.2.3-1"}, // bump numeric part of prerelease
	}
	for _, tc := range tests {
		res, err := bumpVersion(tc.version, tc.bump)
		if err != nil {
			t.Errorf("bumpVersion(%q, %q) returned error: %v", tc.version, tc.bump, err)
			continue
		}
		if res != tc.expected {
			t.Errorf("bumpVersion(%q, %q) = %q, expected %q", tc.version, tc.bump, res, tc.expected)
		}
	}
	// Verify that an unknown bump argument returns an error.
	if _, err := bumpVersion("v1.2.3", "unknown"); err == nil {
		t.Error("bumpVersion with unknown bump argument did not return error")
	}
}

// TestReadWriteVersionFile tests the file I/O helpers for the version file.
func TestReadWriteVersionFile(t *testing.T) {
	// Create a temporary directory.
	tmpDir, err := os.MkdirTemp("", "goversion_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Case 1: File does not exist; readCurrentVersion should create it.
	versionFilePath := filepath.Join(tmpDir, "new_version.go")
	// The file does not exist so we expect to receive the default "dev".
	version, err := readCurrentVersion(versionFilePath)
	if err != nil {
		t.Fatalf("readCurrentVersion failed: %v", err)
	}
	if version != "dev" {
		t.Errorf("expected default version \"dev\", got %q", version)
	}
	// Verify that the file was created and contains a proper package declaration.
	data, err := os.ReadFile(versionFilePath)
	if err != nil {
		t.Fatalf("failed to read newly created version file: %v", err)
	}
	if !strings.Contains(string(data), "package ") {
		t.Errorf("expected a package declaration in new version file; got: %s", string(data))
	}

	// Case 2: File exists. Write a file with a given version and then read it.
	existingFilePath := filepath.Join(tmpDir, "version.go")
	initialVersion := "1.2.3"
	if err := writeVersionFile(existingFilePath, initialVersion); err != nil {
		t.Fatalf("writeVersionFile failed: %v", err)
	}

	readVersion, err := readCurrentVersion(existingFilePath)
	if err != nil {
		t.Fatalf("readCurrentVersion failed: %v", err)
	}
	if readVersion != initialVersion {
		t.Errorf("read version = %q, expected %q", readVersion, initialVersion)
	}
}

// TestGitIntegration is an integration test that creates a temporary git repository,
// writes a version file, and runs a bump operation using Run.
// This test is skipped if git is not available.
func TestGitIntegration(t *testing.T) {
	if err := checkGit(); err != nil {
		t.Skip("git is not available on system")
	}

	// Create a temporary directory to serve as our git repository.
	tmpDir, err := os.MkdirTemp("", "goversion_git_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize a new git repository.
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v, output: %s", err, string(output))
	}

	// Configure git user so that commits succeed.
	configCmds := [][]string{
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test User"},
	}
	for _, args := range configCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config failed: %v, output: %s", err, string(output))
		}
	}

	// Create the directory for the version file.
	verDir := filepath.Join(tmpDir, "pkg", "version")
	if err := os.MkdirAll(verDir, 0755); err != nil {
		t.Fatalf("failed to create version directory: %v", err)
	}

	// Write the initial version file with version "1.2.3".
	versionFilePath := filepath.Join(verDir, "version.go")
	initialVersion := "1.2.3"
	if err := writeVersionFile(versionFilePath, initialVersion); err != nil {
		t.Fatalf("writeVersionFile failed: %v", err)
	}

	// Stage and commit the initial version file so the repo has at least one commit.
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add initial failed: %v, output: %s", err, string(output))
	}
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit initial failed: %v, output: %s", err, string(output))
	}

	// Change the working directory to the temp repo.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Run the version bump. For example, bump the "patch" version.
	// Pass the version file path to Run and also include it in the extra files list.
	if _, err := Run(versionFilePath, "patch", []string{versionFilePath}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify that the version file was updated to "1.2.4".
	newVersion, err := readCurrentVersion(versionFilePath)
	if err != nil {
		t.Fatalf("readCurrentVersion after bump failed: %v", err)
	}
	if newVersion != "1.2.4" {
		t.Errorf("after bump, version file = %q, expected %q", newVersion, "1.2.4")
	}

	// Check that a git tag "v1.2.4" was created.
	cmd = exec.Command("git", "tag")
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag failed: %v, output: %s", err, string(output))
	}
	tags := strings.Split(strings.TrimSpace(string(output)), "\n")
	expectedTag := "v1.2.4"
	if !slices.Contains(tags, expectedTag) {
		t.Errorf("expected git tag %q not found; got tags: %v", expectedTag, tags)
	}
}

// TestExplicitVersion tests providing an explicit version instead of a bump keyword.
func TestExplicitVersion(t *testing.T) {
	// Create a temporary directory for the version file and git repository.
	tmpDir, err := os.MkdirTemp("", "explicit_version_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	versionFilePath := filepath.Join(tmpDir, "version.go")
	initialVersion := "1.2.3"
	if err := writeVersionFile(versionFilePath, initialVersion); err != nil {
		t.Fatalf("writeVersionFile failed: %v", err)
	}

	// Initialize a git repository in the temporary directory.
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v, output: %s", err, string(output))
	}
	// Configure git.
	configCmds := [][]string{
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test User"},
	}
	for _, args := range configCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config failed: %v, output: %s", err, string(output))
		}
	}
	// Stage and commit the file.
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v, output: %s", err, string(output))
	}
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v, output: %s", err, string(output))
	}

	// Change the current working directory to tmpDir so that git commands in Run are executed there.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Run with an explicit version (e.g., bumping directly to 2.0.0).
	explicitVersion := "2.0.0"
	if _, err := Run(versionFilePath, explicitVersion, []string{versionFilePath}); err != nil {
		t.Fatalf("Run with explicit version failed: %v", err)
	}

	// Verify that the version file was updated.
	updatedVersion, err := readCurrentVersion(versionFilePath)
	if err != nil {
		t.Fatalf("readCurrentVersion after explicit version bump failed: %v", err)
	}
	if updatedVersion != explicitVersion {
		t.Errorf("after explicit version bump, version file = %q, expected %q", updatedVersion, explicitVersion)
	}

	// Check that a git tag "v2.0.0" was created.
	cmd = exec.Command("git", "tag")
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag failed: %v, output: %s", err, string(output))
	}
	tags := strings.Split(strings.TrimSpace(string(output)), "\n")
	expectedTag := "v2.0.0"

	if !slices.Contains(tags, expectedTag) {
		t.Errorf("expected git tag %q not found; got tags: %v", expectedTag, tags)
	}
}

// TestRejectsDirtyWorkingDir ensures Run fails if uncommitted changes are present outside allowed files.
func TestRejectsDirtyWorkingDir(t *testing.T) {
	if err := checkGit(); err != nil {
		t.Skip("git is not available on system")
	}

	tmpDir, err := os.MkdirTemp("", "goversion_dirty_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v, output: %s", err, string(output))
	}

	// Git identity
	configCmds := [][]string{
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test User"},
	}
	for _, args := range configCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config failed: %v, output: %s", err, string(output))
		}
	}

	// Create version.go
	versionPath := filepath.Join(tmpDir, "version.go")
	if err := writeVersionFile(versionPath, "1.2.3"); err != nil {
		t.Fatalf("writeVersionFile failed: %v", err)
	}

	// Create unrelated dirty file
	dirtyPath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(dirtyPath, []byte("unsaved changes\n"), 0644); err != nil {
		t.Fatalf("failed to write dirty file: %v", err)
	}

	// Stage and commit version.go
	cmd = exec.Command("git", "add", "version.go")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v, output: %s", err, string(output))
	}
	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v, output: %s", err, string(output))
	}

	// Run goversion bump with dirty README.md
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	_, err = Run(versionPath, "patch", []string{versionPath})
	if err == nil || !strings.Contains(err.Error(), "working directory is dirty") {
		t.Errorf("expected error due to dirty working directory, got: %v", err)
	}
}

// TestDryRun validates that DryRun returns the expected metadata and does not update the version file.
func TestDryRun(t *testing.T) {
	// Create a temporary directory.
	tmpDir, err := os.MkdirTemp("", "dryrun_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a temporary version file with an initial version.
	versionFilePath := filepath.Join(tmpDir, "version.go")
	initialVersion := "1.2.3"
	if err := writeVersionFile(versionFilePath, initialVersion); err != nil {
		t.Fatalf("writeVersionFile failed: %v", err)
	}

	// Execute DryRun with a "minor" bump.
	meta, err := DryRun(versionFilePath, "minor")
	if err != nil {
		t.Fatalf("DryRun failed: %v", err)
	}

	// Expect a minor bump: version "1.2.3" should become "1.3.0".
	if meta.OldVersion != initialVersion {
		t.Errorf("expected OldVersion %q, got %q", initialVersion, meta.OldVersion)
	}
	expectedNewVersion := "1.3.0"
	if meta.NewVersion != expectedNewVersion {
		t.Errorf("expected NewVersion %q, got %q", expectedNewVersion, meta.NewVersion)
	}
	if meta.BumpType != "minor" {
		t.Errorf("expected BumpType %q, got %q", "minor", meta.BumpType)
	}

	// Verify that DryRun does not update the version file.
	currentVersion, err := readCurrentVersion(versionFilePath)
	if err != nil {
		t.Fatalf("readCurrentVersion failed: %v", err)
	}
	if currentVersion != initialVersion {
		t.Errorf("DryRun should not update the file; expected version %q, got %q", initialVersion, currentVersion)
	}
}
