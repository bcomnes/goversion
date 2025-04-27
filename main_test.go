// cli_test.go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain triggers the CLI as a subprocess when GO_HELPER_PROCESS is set.
func TestMain(m *testing.M) {
	if os.Getenv("GO_HELPER_PROCESS") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runCLI runs the CLI in helper process mode with optional extra environment vars.
func runCLI(args []string, extraEnv ...string) (string, error) {
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "GO_HELPER_PROCESS=1")
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCLIHelp(t *testing.T) {
	out, _ := runCLI([]string{"-help"})
	if !strings.Contains(out, "Usage:") {
		t.Errorf("expected help output, got:\n%s", out)
	}
}

func TestCLIVersionFlag(t *testing.T) {
	out, _ := runCLI([]string{"-version"})
	if !strings.Contains(out, Version) {
		t.Errorf("expected CLI version in output, got:\n%s", out)
	}
}

func TestCLIMissingVersionArg(t *testing.T) {
	out, _ := runCLI([]string{})
	if !strings.Contains(out, "Error: <version-bump> positional argument is required") {
		t.Errorf("expected missing positional argument error, got:\n%s", out)
	}
}

func TestCLIPatchBumpIntegration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "goversion_cli_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	// Init repo and config user
	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")

	// Create version.go
	versionDir := filepath.Join(tmpDir, "pkg")
	err = os.MkdirAll(versionDir, 0755)
	if err != nil {
		t.Fatalf("failed to create pkg dir: %v", err)
	}
	relativeVersionFile := filepath.Join("pkg", "version.go")
	absVersionFile := filepath.Join(tmpDir, relativeVersionFile)

	initial := `package version

var (
	Version = "1.2.3"
)
`
	if err := os.WriteFile(absVersionFile, []byte(initial), 0644); err != nil {
		t.Fatalf("failed to write version file: %v", err)
	}

	// Commit initial version
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	// Run CLI in tmpDir
	cmd := exec.Command(os.Args[0], "-version-file", relativeVersionFile, "patch")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "GO_HELPER_PROCESS=1")
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI failed: %v\nstdout/stderr:\n%s", err, out)
	}

	// Check updated version.go
	contents, err := os.ReadFile(absVersionFile)
	if err != nil {
		t.Fatalf("reading version file failed: %v", err)
	}
	if !strings.Contains(string(contents), `Version = "1.2.4"`) {
		t.Errorf("expected bumped version, got:\n%s", contents)
	}

	// Check git tag
	cmd = exec.Command("git", "tag")
	cmd.Dir = tmpDir
	tagsOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag failed: %v\n%s", err, tagsOut)
	}
	if !strings.Contains(string(tagsOut), "v1.2.4") {
		t.Errorf("expected tag 'v1.2.4' not found. Tags:\n%s", tagsOut)
	}
}

// TestCLIDryRunIntegration tests that the CLI dry run mode computes the correct version bump
// but does not update the version file or commit any changes.
func TestCLIDryRunIntegration(t *testing.T) {
	// Create a temporary directory.
	tmpDir, err := os.MkdirTemp("", "goversion_cli_dryrun_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Helper function to run git commands in tmpDir.
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	// Initialize a new git repository and configure user.
	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")

	// Create the version file.
	versionDir := filepath.Join(tmpDir, "pkg")
	err = os.MkdirAll(versionDir, 0755)
	if err != nil {
		t.Fatalf("failed to create pkg directory: %v", err)
	}
	relativeVersionFile := filepath.Join("pkg", "version.go")
	absVersionFile := filepath.Join(tmpDir, relativeVersionFile)
	initialContent := `package version

var (
	Version = "1.2.3"
)
`
	if err := os.WriteFile(absVersionFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write version file: %v", err)
	}

	// Stage and commit the initial version.
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	// Run the CLI with the -dry flag.
	cmd := exec.Command(os.Args[0], "-version-file", relativeVersionFile, "-dry", "patch")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "GO_HELPER_PROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI dry run failed: %v\nOutput:\n%s", err, out)
	}

	// Verify that the dry run output shows the computed metadata.
	if !strings.Contains(string(out), "Old Version: 1.2.3") {
		t.Errorf("expected output to contain 'Old Version: 1.2.3', got:\n%s", out)
	}
	if !strings.Contains(string(out), "New Version: 1.2.4") {
		t.Errorf("expected output to contain 'New Version: 1.2.4', got:\n%s", out)
	}

	// Confirm that the version file has not been changed.
	contents, err := os.ReadFile(absVersionFile)
	if err != nil {
		t.Fatalf("reading version file failed: %v", err)
	}
	if !strings.Contains(string(contents), `Version = "1.2.3"`) {
		t.Errorf("dry run should not update the version file; got:\n%s", string(contents))
	}

	// Verify that no git tags were created.
	cmd = exec.Command("git", "tag")
	cmd.Dir = tmpDir
	tagsOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag failed: %v\n%s", err, tagsOut)
	}
	if strings.Contains(string(tagsOut), "v1.2.4") {
		t.Errorf("dry run should not create a git tag; got tags:\n%s", tagsOut)
	}
}

func TestCLIMajorBumpIntegration(t *testing.T) {
    tmpDir, err := os.MkdirTemp("", "goversion_cli_major_test")
    if err != nil {
        t.Fatal(err)
    }
    defer os.RemoveAll(tmpDir)

    runGit := func(args ...string) {
        cmd := exec.Command("git", args...)
        cmd.Dir = tmpDir
        if out, err := cmd.CombinedOutput(); err != nil {
            t.Fatalf("git %v failed: %v\n%s", args, err, out)
        }
    }

    // init repo + config
    runGit("init")
    runGit("config", "user.email", "test@example.com")
    runGit("config", "user.name", "Test User")

    // write a simple go.mod
    modContent := `module example.com/m

go 1.18
`
    if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(modContent), 0644); err != nil {
        t.Fatalf("failed to write go.mod: %v", err)
    }

    // create the version file
    versionDir := filepath.Join(tmpDir, "pkg")
    if err := os.MkdirAll(versionDir, 0755); err != nil {
        t.Fatalf("failed to mkdir pkg: %v", err)
    }
    rel := filepath.Join("pkg", "version.go")
    abs := filepath.Join(tmpDir, rel)
    initial := `package version

var (
    Version = "1.2.3"
)
`
    if err := os.WriteFile(abs, []byte(initial), 0644); err != nil {
        t.Fatalf("write version.go: %v", err)
    }

    // commit both files
    runGit("add", ".")
    runGit("commit", "-m", "initial")

    // run CLI with "major"
    cmd := exec.Command(os.Args[0], "-version-file", rel, "major")
    cmd.Dir = tmpDir
    cmd.Env = append(os.Environ(),
        "GO_HELPER_PROCESS=1",
        "GIT_AUTHOR_NAME=Test User",
        "GIT_AUTHOR_EMAIL=test@example.com",
        "GIT_COMMITTER_NAME=Test User",
        "GIT_COMMITTER_EMAIL=test@example.com",
    )
    out, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("CLI major bump failed: %v\n%s", err, out)
    }

    // check version.go
    got, err := os.ReadFile(abs)
    if err != nil {
        t.Fatalf("read version.go failed: %v", err)
    }
    if !strings.Contains(string(got), `Version = "2.0.0"`) {
        t.Errorf("version.go =\n%s\nwant Version = \"2.0.0\"", got)
    }

    // check go.mod
    modGot, err := os.ReadFile(filepath.Join(tmpDir, "go.mod"))
    if err != nil {
        t.Fatalf("read go.mod failed: %v", err)
    }
    first := strings.SplitN(string(modGot), "\n", 2)[0]
    if !strings.Contains(first, "/v2") {
        t.Errorf("go.mod first line = %q; want it to include \"/v2\"", first)
    }

    // check git tag
    // check git tag
    cmd = exec.Command("git", "tag")
    cmd.Dir = tmpDir
    tagsOut, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("git tag failed: %v\n%s", err, tagsOut)
    }
    if !strings.Contains(string(tagsOut), "v2.0.0") {
        t.Errorf("git tags = %s; want v2.0.0", tagsOut)
    }
}
