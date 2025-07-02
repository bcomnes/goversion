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
	if _, err := Run(versionFilePath, "patch", []string{versionFilePath}, nil); err != nil {
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
	if _, err := Run(versionFilePath, explicitVersion, []string{versionFilePath}, nil); err != nil {
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

	_, err = Run(versionPath, "patch", []string{versionPath}, nil)
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
	meta, err := DryRun(versionFilePath, "minor", nil)
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

    // TestBumpInFilesIntegration tests the bump-in functionality that updates versions in arbitrary files
    func TestBumpInFilesIntegration(t *testing.T) {
    	if err := checkGit(); err != nil {
    		t.Skip("git is not available on system")
    	}

    	// Create a temporary directory
    	tmpDir, err := os.MkdirTemp("", "goversion_bumpin_test")
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
    	versionPath := filepath.Join(tmpDir, "version.go")
    	if err := writeVersionFile(versionPath, "1.2.3"); err != nil {
    		t.Fatalf("writeVersionFile failed: %v", err)
    	}

    	// Create package.json
    	packagePath := filepath.Join(tmpDir, "package.json")
    	packageContent := `{
      "name": "my-app",
      "version": "1.2.3",
      "description": "Test application"
    }`
    	if err := os.WriteFile(packagePath, []byte(packageContent), 0644); err != nil {
    		t.Fatalf("writing package.json failed: %v", err)
    	}

    	// Create README.md
    	readmePath := filepath.Join(tmpDir, "README.md")
    	readmeContent := `# My App

    Current version: v1.2.3

    ## Installation

    Install version 1.2.3 with: npm install my-app@1.2.3`
    	if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
    		t.Fatalf("writing README.md failed: %v", err)
    	}

    	// Stage and commit initial files
    	cmd = exec.Command("git", "add", ".")
    	cmd.Dir = tmpDir
    	if output, err := cmd.CombinedOutput(); err != nil {
    		t.Fatalf("git add failed: %v, output: %s", err, string(output))
    	}
    	cmd = exec.Command("git", "commit", "-m", "initial")
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

    	// Run goversion with bump-in files
    	bumpInFiles := []string{packagePath, readmePath}
    	meta, err := Run(versionPath, "minor", []string{versionPath}, bumpInFiles)
    	if err != nil {
    		t.Fatalf("Run with bump-in failed: %v", err)
    	}

    	// Verify metadata
    	if meta.OldVersion != "1.2.3" {
    		t.Errorf("expected old version 1.2.3, got %s", meta.OldVersion)
    	}
    	if meta.NewVersion != "1.3.0" {
    		t.Errorf("expected new version 1.3.0, got %s", meta.NewVersion)
    	}

    	// Verify version.go was updated
    	versionContent, err := os.ReadFile(versionPath)
    	if err != nil {
    		t.Fatal(err)
    	}
    	if !strings.Contains(string(versionContent), `Version = "1.3.0"`) {
    		t.Errorf("version.go not updated correctly:\n%s", string(versionContent))
    	}

    	// Verify package.json was updated
    	packageResult, err := os.ReadFile(packagePath)
    	if err != nil {
    		t.Fatal(err)
    	}
    	if !strings.Contains(string(packageResult), `"version": "1.3.0"`) {
    		t.Errorf("package.json not updated correctly:\n%s", string(packageResult))
    	}

    	// Verify README.md was updated
    	readmeResult, err := os.ReadFile(readmePath)
    	if err != nil {
    		t.Fatal(err)
    	}
    	if !strings.Contains(string(readmeResult), "Current version: v1.3.0") {
    		t.Errorf("README.md version with v prefix not updated correctly:\n%s", string(readmeResult))
    	}
    	if !strings.Contains(string(readmeResult), "Install version 1.3.0") {
    		t.Errorf("README.md version without v prefix not updated correctly:\n%s", string(readmeResult))
    	}

    	// Verify git tag
    	cmd = exec.Command("git", "tag")
    	cmd.Dir = tmpDir
    	tagsOut, err := cmd.CombinedOutput()
    	if err != nil {
    		t.Fatalf("git tag failed: %v\n%s", err, tagsOut)
    	}
    	if !strings.Contains(string(tagsOut), "v1.3.0") {
    		t.Errorf("expected tag v1.3.0 not found. Tags:\n%s", tagsOut)
    	}

    	// Verify all files were committed
    	cmd = exec.Command("git", "status", "--porcelain")
    	cmd.Dir = tmpDir
    	statusOut, err := cmd.CombinedOutput()
    	if err != nil {
    		t.Fatalf("git status failed: %v\n%s", err, statusOut)
    	}
    	if len(statusOut) > 0 {
    		t.Errorf("uncommitted changes found:\n%s", statusOut)
    	}
    }

    // TestDryRunWithBumpIn tests that dry run correctly identifies files that would be modified
    func TestDryRunWithBumpIn(t *testing.T) {
    	// Create a temporary directory
    	tmpDir, err := os.MkdirTemp("", "goversion_dryrun_bumpin_test")
    	if err != nil {
    		t.Fatal(err)
    	}
    	defer os.RemoveAll(tmpDir)

    	// Create version.go
    	versionPath := filepath.Join(tmpDir, "version.go")
    	if err := writeVersionFile(versionPath, "2.1.0"); err != nil {
    		t.Fatalf("writeVersionFile failed: %v", err)
    	}

    	// Create a file with version
    	configPath := filepath.Join(tmpDir, "config.yaml")
    	configContent := `app:
      name: MyApp
      version: "2.1.0"
      port: 8080`
    	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
    		t.Fatal(err)
    	}

    	// Create a file without version
    	licensePath := filepath.Join(tmpDir, "LICENSE")
    	licenseContent := `MIT License

    Copyright (c) 2024 Test Author`
    	if err := os.WriteFile(licensePath, []byte(licenseContent), 0644); err != nil {
    		t.Fatal(err)
    	}

    	// Run dry run
    	bumpInFiles := []string{configPath, licensePath}
    	meta, err := DryRun(versionPath, "patch", bumpInFiles)
    	if err != nil {
    		t.Fatalf("DryRun failed: %v", err)
    	}

    	// Check metadata
    	if meta.OldVersion != "2.1.0" {
    		t.Errorf("expected old version 2.1.0, got %s", meta.OldVersion)
    	}
    	if meta.NewVersion != "2.1.1" {
    		t.Errorf("expected new version 2.1.1, got %s", meta.NewVersion)
    	}

    	// Check UpdatedFiles includes version.go and config.yaml but not LICENSE
    	if !slices.Contains(meta.UpdatedFiles, versionPath) {
    		t.Errorf("version.go should be in UpdatedFiles")
    	}
    	if !slices.Contains(meta.UpdatedFiles, configPath) {
    		t.Errorf("config.yaml should be in UpdatedFiles (has version)")
    	}
    	if slices.Contains(meta.UpdatedFiles, licensePath) {
    		t.Errorf("LICENSE should not be in UpdatedFiles (no version)")
    	}

    	// Verify files were not actually modified
    	versionContent, _ := os.ReadFile(versionPath)
    	if !strings.Contains(string(versionContent), `Version = "2.1.0"`) {
    		t.Errorf("version.go should not be modified in dry run")
    	}
    	configResult, _ := os.ReadFile(configPath)
    	if !strings.Contains(string(configResult), `version: "2.1.0"`) {
    		t.Errorf("config.yaml should not be modified in dry run")
    	}
    }

// TestExtensionTOMLIntegration tests bumping version in extension.toml files
func TestExtensionTOMLIntegration(t *testing.T) {
	if err := checkGit(); err != nil {
		t.Skip("git is not available on system")
	}

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "goversion_extension_toml_test")
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
	versionPath := filepath.Join(tmpDir, "version.go")
	if err := writeVersionFile(versionPath, "0.1.0"); err != nil {
		t.Fatalf("writeVersionFile failed: %v", err)
	}

	// Create extension.toml
	extensionPath := filepath.Join(tmpDir, "extension.toml")
	extensionContent := `[package]
name = "my-vscode-extension"
version = "0.1.0"
authors = ["Extension Author"]
repository = "https://github.com/test/extension"

[scripts]
build = "npm run compile"
test = "npm test"

[engines]
vscode = "^1.70.0"`
	if err := os.WriteFile(extensionPath, []byte(extensionContent), 0644); err != nil {
		t.Fatalf("writing extension.toml failed: %v", err)
	}

	// Create Cargo.toml as another TOML example
	cargoPath := filepath.Join(tmpDir, "Cargo.toml")
	cargoContent := `[package]
name = "rust-extension"
version = "0.1.0"
edition = "2021"

[dependencies]
serde = "1.0"`
	if err := os.WriteFile(cargoPath, []byte(cargoContent), 0644); err != nil {
		t.Fatalf("writing Cargo.toml failed: %v", err)
	}

	// Stage and commit initial files
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v, output: %s", err, string(output))
	}
	cmd = exec.Command("git", "commit", "-m", "initial")
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

	// Run goversion with bump-in files
	bumpInFiles := []string{extensionPath, cargoPath}
	meta, err := Run(versionPath, "1.0.0", []string{versionPath}, bumpInFiles)
	if err != nil {
		t.Fatalf("Run with TOML files failed: %v", err)
	}

	// Verify metadata
	if meta.OldVersion != "0.1.0" {
		t.Errorf("expected old version 0.1.0, got %s", meta.OldVersion)
	}
	if meta.NewVersion != "1.0.0" {
		t.Errorf("expected new version 1.0.0, got %s", meta.NewVersion)
	}

	// Verify extension.toml was updated
	extensionResult, err := os.ReadFile(extensionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(extensionResult), `version = "1.0.0"`) {
		t.Errorf("extension.toml not updated correctly:\n%s", string(extensionResult))
	}

	// Verify Cargo.toml was updated
	cargoResult, err := os.ReadFile(cargoPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cargoResult), `version = "1.0.0"`) {
		t.Errorf("Cargo.toml not updated correctly:\n%s", string(cargoResult))
	}

	// Verify git tag
	cmd = exec.Command("git", "tag")
	cmd.Dir = tmpDir
	tagsOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag failed: %v\n%s", err, tagsOut)
	}
	if !strings.Contains(string(tagsOut), "v1.0.0") {
		t.Errorf("expected tag v1.0.0 not found. Tags:\n%s", tagsOut)
	}
}
