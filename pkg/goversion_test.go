package goversion

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
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
	if _, err := Run(versionFilePath, "patch", []string{versionFilePath}, []string{}); err != nil {
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
	if _, err := Run(versionFilePath, explicitVersion, []string{versionFilePath}, []string{}); err != nil {
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

	_, err = Run(versionPath, "patch", []string{versionPath}, []string{})
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
	meta, err := DryRun(versionFilePath, "minor", []string{})
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

// TestUpdateGoModSuffix verifies that updateGoMod
// leaves the module path unchanged for v1,
// but appends /vN for majors ≥ 2.
func TestUpdateGoModSuffix(t *testing.T) {
    tmpDir, err := os.MkdirTemp("", "goversion_mod_test")
    if err != nil {
        t.Fatal(err)
    }
    defer os.RemoveAll(tmpDir)

    // A minimal go.mod to start from
    initial := `module example.com/m

go 1.18
`
    modFile := filepath.Join(tmpDir, "go.mod")

    tests := []struct {
        newVersion         string
        expectedModuleLine string
    }{
        {"1.0.0", "module example.com/m"},
        {"2.0.0", "module example.com/m/v2"},
        {"3.0.0", "module example.com/m/v3"},
    }

    for _, tc := range tests {
        // Reset go.mod
        if err := os.WriteFile(modFile, []byte(initial), 0644); err != nil {
            t.Fatalf("writing go.mod for %q: %v", tc.newVersion, err)
        }
        // Run the suffix updater
        if err := updateGoMod(tmpDir, tc.newVersion); err != nil {
            t.Errorf("updateGoMod(%q) error: %v", tc.newVersion, err)
            continue
        }
        // Read back and verify the module line
        data, err := os.ReadFile(modFile)
        if err != nil {
            t.Errorf("reading go.mod for %q: %v", tc.newVersion, err)
            continue
        }
        firstLine := strings.SplitN(string(data), "\n", 2)[0]
        if firstLine != tc.expectedModuleLine {
            t.Errorf("for version %q, got %q; want %q",
                tc.newVersion, firstLine, tc.expectedModuleLine)
        }
    }
}

// TestUpdateSelfImportsIntegration ensures that after a v2 bump,
// imports in other packages under the same module are rewritten.
func TestUpdateSelfImportsIntegration(t *testing.T) {
    // 1) Setup a temporary module
    tmpDir, err := os.MkdirTemp("", "selfimports_test")
    if err != nil {
        t.Fatal(err)
    }
    defer os.RemoveAll(tmpDir)

    // write go.mod for module example.com/foo
    modContents := `module example.com/foo

go 1.18
`
    modFile := filepath.Join(tmpDir, "go.mod")
    if err := os.WriteFile(modFile, []byte(modContents), 0644); err != nil {
        t.Fatalf("writing go.mod: %v", err)
    }

    // 2) Create pkg/a/a.go
    aDir := filepath.Join(tmpDir, "pkg", "a")
    if err := os.MkdirAll(aDir, 0755); err != nil {
        t.Fatal(err)
    }
    aSrc := `package a

func A() {}
`
    if err := os.WriteFile(filepath.Join(aDir, "a.go"), []byte(aSrc), 0644); err != nil {
        t.Fatal(err)
    }

    // 3) Create pkg/b/b.go importing example.com/foo/pkg/a
    bDir := filepath.Join(tmpDir, "pkg", "b")
    if err := os.MkdirAll(bDir, 0755); err != nil {
        t.Fatal(err)
    }
    bSrc := `package b

import "example.com/foo/pkg/a"

func B() { a.A() }
`
    bPath := filepath.Join(bDir, "b.go")
    if err := os.WriteFile(bPath, []byte(bSrc), 0644); err != nil {
        t.Fatal(err)
    }

    // 4) Bump go.mod to v2 (via updateGoMod) and re-parse new module path
    if err := updateGoMod(tmpDir, "2.0.0"); err != nil {
        t.Fatalf("updateGoMod failed: %v", err)
    }
    data, err := os.ReadFile(modFile)
    if err != nil {
        t.Fatalf("reading bumped go.mod: %v", err)
    }
    mf, err := modfile.Parse("go.mod", data, nil)
    if err != nil {
        t.Fatalf("parsing bumped go.mod: %v", err)
    }
    newModPath := mf.Module.Mod.Path // should be "example.com/foo/v2"

    // 5) Rewrite self-imports and collect modified files
    modified, err := updateSelfImports(tmpDir, "example.com/foo", newModPath)
    if err != nil {
        t.Fatalf("updateSelfImports failed: %v", err)
    }

    // 6) Only pkg/b/b.go should have been touched
    if !slices.Contains(modified, bPath) {
        t.Errorf("expected %q in modified list, got: %v", bPath, modified)
    }
    if slices.Contains(modified, filepath.Join(aDir, "a.go")) {
        t.Errorf("pkg/a/a.go should not be rewritten, but was")
    }

    // 7) Verify that b.go’s import line is updated to example.com/foo/v2/pkg/a
    out, err := os.ReadFile(bPath)
    if err != nil {
        t.Fatalf("reading updated b.go: %v", err)
    }
    wantImport := fmt.Sprintf(`import "%s/pkg/a"`, newModPath)
    if !strings.Contains(string(out), wantImport) {
        t.Errorf("b.go import not updated, expected %q; got:\n%s", wantImport, string(out))
    }
}

// TestFindAndReplaceSemver tests the findAndReplaceSemver function with various file formats.
func TestFindAndReplaceSemver(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		newVersion  string
		wantContent string
		wantErr     bool
	}{
		{
			name: "package.json",
			content: `{
  "name": "my-package",
  "version": "1.2.3",
  "dependencies": {
    "express": "4.18.0",
    "lodash": "4.17.21"
  }
}`,
			newVersion: "2.0.0",
			wantContent: `{
  "name": "my-package",
  "version": "2.0.0",
  "dependencies": {
    "express": "4.18.0",
    "lodash": "4.17.21"
  }
}`,
		},
		{
			name: "Cargo.toml",
			content: `[package]
name = "my-crate"
version = "0.1.0"

[dependencies]
serde = "1.0.130"
tokio = { version = "1.21.0", features = ["full"] }`,
			newVersion: "1.0.0",
			wantContent: `[package]
name = "my-crate"
version = "1.0.0"

[dependencies]
serde = "1.0.130"
tokio = { version = "1.21.0", features = ["full"] }`,
		},
		{
			name: "extension.toml",
			content: `id = "zed-theme-tron-legacy"
name = "Tron Legacy"
version = "1.0.1"
schema_version = 1`,
			newVersion: "1.1.0",
			wantContent: `id = "zed-theme-tron-legacy"
name = "Tron Legacy"
version = "1.1.0"
schema_version = 1`,
		},
		{
			name: "pyproject.toml",
			content: `[tool.poetry]
name = "my-package"
version = "2.1.0"

[tool.poetry.dependencies]
python = "3.8"
requests = "2.28.0"`,
			newVersion: "3.0.0",
			wantContent: `[tool.poetry]
name = "my-package"
version = "3.0.0"

[tool.poetry.dependencies]
python = "3.8"
requests = "2.28.0"`,
		},
		{
			name: "prerelease version",
			content: `version = "1.0.0-alpha.1"
other = "1.0.0"`,
			newVersion: "1.0.0-beta.1",
			wantContent: `version = "1.0.0-beta.1"
other = "1.0.0"`,
		},
		{
			name: "version with build metadata",
			content: `version: 1.2.3+build.123
next: 2.0.0`,
			newVersion: "1.2.4",
			wantContent: `version: 1.2.4
next: 2.0.0`,
		},
		{
			name: "complex prerelease and build metadata 1",
			content: `version = "1.0.0-alpha+001"
description = "Alpha release with build number"`,
			newVersion: "1.0.0-beta.1",
			wantContent: `version = "1.0.0-beta.1"
description = "Alpha release with build number"`,
		},
		{
			name: "complex build metadata with timestamp",
			content: `{
  "version": "1.0.0+20130313144700",
  "build": "automated"
}`,
			newVersion: "1.0.1",
			wantContent: `{
  "version": "1.0.1",
  "build": "automated"
}`,
		},
		{
			name: "prerelease with complex build metadata",
			content: `[package]
version = "1.0.0-beta+exp.sha.5114f85"
name = "experimental"`,
			newVersion: "1.0.0",
			wantContent: `[package]
version = "1.0.0"
name = "experimental"`,
		},
		{
			name: "build metadata with special characters",
			content: `app_version: 1.0.0+21AF26D3----117B344092BD
status: deployed`,
			newVersion: "1.1.0",
			wantContent: `app_version: 1.1.0
status: deployed`,
		},
		{
			name: "multiple complex versions only first replaced",
			content: `versions:
  current: 2.1.0-rc.1+build.123
  previous: 2.0.0+20130313144700
  legacy: 1.9.9-beta+exp.sha.5114f85`,
			newVersion: "2.1.0",
			wantContent: `versions:
  current: 2.1.0
  previous: 2.0.0+20130313144700
  legacy: 1.9.9-beta+exp.sha.5114f85`,
		},
		{
			name: "zero-padded numeric prerelease",
			content: `release = "1.0.0-0.3.7"`,
			newVersion: "1.0.0-0.3.8",
			wantContent: `release = "1.0.0-0.3.8"`,
		},
		{
			name: "complex prerelease identifiers",
			content: `version: "1.0.0-x.7.z.92"`,
			newVersion: "1.0.0-x.7.z.93",
			wantContent: `version: "1.0.0-x.7.z.93"`,
		},
		{
			name: "prerelease with hyphens",
			content: `{"version": "1.0.0-x-y-z.--"}`,
			newVersion: "1.0.0",
			wantContent: `{"version": "1.0.0"}`,
		},
		{
			name: "semver.org example 1",
			content: `version = "1.0.0-alpha"`,
			newVersion: "1.0.0-alpha.1",
			wantContent: `version = "1.0.0-alpha.1"`,
		},
		{
			name: "semver.org example 2",
			content: `version = "1.0.0-alpha.1"`,
			newVersion: "1.0.0-alpha.beta",
			wantContent: `version = "1.0.0-alpha.beta"`,
		},
		{
			name: "semver.org example 3",
			content: `version = "1.0.0-0.3.7"`,
			newVersion: "1.0.0-rc.1",
			wantContent: `version = "1.0.0-rc.1"`,
		},
		{
			name: "semver.org example 4",
			content: `version = "1.0.0-x.7.z.92"`,
			newVersion: "1.0.0",
			wantContent: `version = "1.0.0"`,
		},
		{
			name: "semver.org example 5",
			content: `version = "1.0.0-alpha+001"`,
			newVersion: "1.0.0",
			wantContent: `version = "1.0.0"`,
		},
		{
			name: "semver.org example 6",
			content: `version = "1.0.0+20130313144700"`,
			newVersion: "1.0.1",
			wantContent: `version = "1.0.1"`,
		},
		{
			name: "semver.org example 7",
			content: `version = "1.0.0-beta+exp.sha.5114f85"`,
			newVersion: "1.0.0-beta.2",
			wantContent: `version = "1.0.0-beta.2"`,
		},
		{
			name:        "no semver found",
			content:     `name = "test"\ndescription = "no version here"`,
			newVersion:  "1.0.0",
			wantContent: `name = "test"\ndescription = "no version here"`,
			wantErr:     true,
		},
		{
			name:        "v-prefixed version ignored",
			content:     `version = "v1.2.3"`,
			newVersion:  "2.0.0",
			wantContent: `version = "v1.2.3"`,
			wantErr:     true,
		},
		{
			name: "multiple versions only first replaced",
			content: `main_version = "1.0.0"
api_version = "2.0.0"
cli_version = "3.0.0"`,
			newVersion: "4.0.0",
			wantContent: `main_version = "4.0.0"
api_version = "2.0.0"
cli_version = "3.0.0"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create temp file
			tmpFile, err := os.CreateTemp("", "test_*.toml")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			// Write initial content
			if err := os.WriteFile(tmpFile.Name(), []byte(tc.content), 0644); err != nil {
				t.Fatalf("failed to write initial content: %v", err)
			}

			// Run findAndReplaceSemver
			err = findAndReplaceSemver(tmpFile.Name(), tc.newVersion)

			// Check error
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			// Check content
			gotContent, err := os.ReadFile(tmpFile.Name())
			if err != nil {
				t.Fatalf("failed to read result: %v", err)
			}

			if string(gotContent) != tc.wantContent {
				t.Errorf("content mismatch\ngot:\n%s\nwant:\n%s", gotContent, tc.wantContent)
			}
		})
	}
}

// TestBumpFilesIntegration tests the full integration of bump files with git.
func TestBumpFilesIntegration(t *testing.T) {
	if err := checkGit(); err != nil {
		t.Skip("git is not available on system")
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "goversion_bump_files_test")
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

	// Configure git
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
	versionFile := filepath.Join(tmpDir, "version.go")
	if err := writeVersionFile(versionFile, "1.2.3"); err != nil {
		t.Fatalf("writeVersionFile failed: %v", err)
	}

	// Create package.json
	packageFile := filepath.Join(tmpDir, "package.json")
	packageContent := `{
  "name": "test-package",
  "version": "1.2.3",
  "dependencies": {
    "express": "4.18.0"
  }
}`
	if err := os.WriteFile(packageFile, []byte(packageContent), 0644); err != nil {
		t.Fatalf("failed to write package.json: %v", err)
	}

	// Create Cargo.toml
	cargoFile := filepath.Join(tmpDir, "Cargo.toml")
	cargoContent := `[package]
name = "test-crate"
version = "1.2.3"

[dependencies]
serde = "1.0.130"`
	if err := os.WriteFile(cargoFile, []byte(cargoContent), 0644); err != nil {
		t.Fatalf("failed to write Cargo.toml: %v", err)
	}

	// Stage and commit initial files
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

	// Change working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Run with bump files
	meta, err := Run(versionFile, "minor", []string{versionFile}, []string{packageFile, cargoFile})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify metadata
	if meta.OldVersion != "1.2.3" {
		t.Errorf("expected OldVersion 1.2.3, got %s", meta.OldVersion)
	}
	if meta.NewVersion != "1.3.0" {
		t.Errorf("expected NewVersion 1.3.0, got %s", meta.NewVersion)
	}

	// Verify all files were updated
	versionContent, _ := readCurrentVersion(versionFile)
	if versionContent != "1.3.0" {
		t.Errorf("version.go not updated correctly: %s", versionContent)
	}

	packageResult, _ := os.ReadFile(packageFile)
	if !strings.Contains(string(packageResult), `"version": "1.3.0"`) {
		t.Errorf("package.json not updated correctly: %s", packageResult)
	}

	cargoResult, _ := os.ReadFile(cargoFile)
	if !strings.Contains(string(cargoResult), `version = "1.3.0"`) {
		t.Errorf("Cargo.toml not updated correctly: %s", cargoResult)
	}

	// Verify git tag
	cmd = exec.Command("git", "tag")
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag failed: %v, output: %s", err, string(output))
	}
	if !strings.Contains(string(output), "v1.3.0") {
		t.Errorf("expected git tag v1.3.0 not found; got tags: %v", string(output))
	}

	// Verify UpdatedFiles includes all bump files
	if !slices.Contains(meta.UpdatedFiles, versionFile) {
		t.Errorf("version.go not in UpdatedFiles")
	}
	if !slices.Contains(meta.UpdatedFiles, packageFile) {
		t.Errorf("package.json not in UpdatedFiles")
	}
	if !slices.Contains(meta.UpdatedFiles, cargoFile) {
		t.Errorf("Cargo.toml not in UpdatedFiles")
	}
}

// TestDryRunWithBumpFiles tests that DryRun correctly identifies bump files.
func TestDryRunWithBumpFiles(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "goversion_dryrun_bump_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create version.go
	versionFile := filepath.Join(tmpDir, "version.go")
	if err := writeVersionFile(versionFile, "1.0.0"); err != nil {
		t.Fatalf("writeVersionFile failed: %v", err)
	}

	// Create a bump file
	bumpFile := filepath.Join(tmpDir, "config.toml")
	bumpContent := `version = "1.0.0"`
	if err := os.WriteFile(bumpFile, []byte(bumpContent), 0644); err != nil {
		t.Fatalf("failed to write bump file: %v", err)
	}

	// Run DryRun
	meta, err := DryRun(versionFile, "major", []string{bumpFile})
	if err != nil {
		t.Fatalf("DryRun failed: %v", err)
	}

	// Verify metadata
	if meta.NewVersion != "2.0.0" {
		t.Errorf("expected NewVersion 2.0.0, got %s", meta.NewVersion)
	}

	// Verify UpdatedFiles includes bump file
	if !slices.Contains(meta.UpdatedFiles, bumpFile) {
		t.Errorf("bump file not in UpdatedFiles: %v", meta.UpdatedFiles)
	}

	// Verify files were NOT actually modified
	content, _ := os.ReadFile(bumpFile)
	if !strings.Contains(string(content), `version = "1.0.0"`) {
		t.Errorf("bump file was modified during dry run")
	}
}
