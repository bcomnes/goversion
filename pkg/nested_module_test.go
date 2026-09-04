package goversion

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNestedModuleRunWithOptionsCreatesCanonicalTag(t *testing.T) {
	root, moduleDir := initNestedModuleRepo(t, "example.com/acme/repo/tools/widget", "1.2.3")

	meta, err := RunWithOptions(VersionOptions{WorkDir: moduleDir}, "patch")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Tag != "tools/widget/v1.2.4" {
		t.Fatalf("Tag = %q; want tools/widget/v1.2.4", meta.Tag)
	}
	output := gitOutput(t, root, "tag", "--list")
	if !strings.Contains(output, meta.Tag) {
		t.Fatalf("tags %q do not contain %q", output, meta.Tag)
	}
}

func TestLegacyAPIsSelectModuleFromNestedVersionFile(t *testing.T) {
	t.Run("Run major migration", func(t *testing.T) {
		root, moduleDir := initNestedModuleRepo(t, "example.com/acme/repo/tools/widget", "1.9.0")
		consumer := filepath.Join(moduleDir, "consumer.go")
		if err := os.WriteFile(consumer, []byte("package widget\n\nimport _ \"example.com/acme/repo/tools/widget/internal/value\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, root, "add", ".")
		gitRun(t, root, "commit", "-m", "add consumer")
		chdirForTest(t, root)

		meta, err := Run("tools/widget/version.go", "major", nil, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if meta.Tag != "tools/widget/v2.0.0" {
			t.Fatalf("Tag = %q; want tools/widget/v2.0.0", meta.Tag)
		}
		goMod, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(goMod), "module example.com/acme/repo/tools/widget/v2") {
			t.Fatalf("nested go.mod was not migrated: %s", goMod)
		}
	})

	t.Run("DryRun tag and migration files", func(t *testing.T) {
		root, moduleDir := initNestedModuleRepo(t, "example.com/acme/repo/tools/widget", "1.9.0")
		chdirForTest(t, root)

		meta, err := DryRun("tools/widget/version.go", "major", nil)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Tag != "tools/widget/v2.0.0" {
			t.Fatalf("Tag = %q; want tools/widget/v2.0.0", meta.Tag)
		}
		if !containsPath(meta.UpdatedFiles, filepath.Join(moduleDir, "go.mod")) {
			t.Fatalf("UpdatedFiles = %v; want nested go.mod", meta.UpdatedFiles)
		}
	})
}

func TestVersionOptionsRejectRepositoryEscapesBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name    string
		options func(root, moduleDir, outside string) VersionOptions
		dryRun  bool
	}{
		{name: "version traversal", options: func(_, moduleDir, _ string) VersionOptions {
			return VersionOptions{WorkDir: moduleDir, VersionFile: "../../../version.go"}
		}},
		{name: "version absolute", options: func(_, moduleDir, outside string) VersionOptions {
			return VersionOptions{WorkDir: moduleDir, VersionFile: filepath.Join(outside, "version.go")}
		}, dryRun: true},
		{name: "extra traversal", options: func(_, moduleDir, _ string) VersionOptions {
			return VersionOptions{WorkDir: moduleDir, ExtraFiles: []string{"../../../extra.txt"}}
		}},
		{name: "bump absolute", options: func(_, moduleDir, outside string) VersionOptions {
			return VersionOptions{WorkDir: moduleDir, BumpFiles: []string{filepath.Join(outside, "bump.txt")}}
		}, dryRun: true},
		{name: "post bump traversal", options: func(_, moduleDir, _ string) VersionOptions {
			return VersionOptions{WorkDir: moduleDir, PostBumpScript: "../../../script.sh"}
		}},
		{name: "existing symlink", options: func(_, moduleDir, _ string) VersionOptions {
			return VersionOptions{WorkDir: moduleDir, ExtraFiles: []string{"escape/extra.txt"}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, moduleDir := initNestedModuleRepo(t, "example.com/acme/repo/tools/widget", "1.2.3")
			outside := t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "version.go"), []byte("package outside\nvar Version = \"9.9.9\"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "bump.txt"), []byte("1.2.3\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(moduleDir, "escape")); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(moduleDir, "version.go"))
			if err != nil {
				t.Fatal(err)
			}

			options := test.options(root, moduleDir, outside)
			var runErr error
			if test.dryRun {
				_, runErr = DryRunWithOptions(options, "patch")
			} else {
				_, runErr = RunWithOptions(options, "patch")
			}
			if runErr == nil || !strings.Contains(runErr.Error(), "outside Git repository") {
				t.Fatalf("expected repository escape error, got %v", runErr)
			}
			after, err := os.ReadFile(filepath.Join(moduleDir, "version.go"))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("version file changed before escape rejection\nbefore: %s\nafter: %s", before, after)
			}
			if tags := gitOutput(t, root, "tag", "--list"); tags != "" {
				t.Fatalf("created tags before escape rejection: %q", tags)
			}
		})
	}
}

func TestNestedModuleDiscoveryUsesModulePrefix(t *testing.T) {
	root, moduleDir := initNestedModuleRepo(t, "example.com/acme/repo/tools/widget", "1.0.0")
	gitRun(t, root, "tag", "v9.0.0")
	gitRun(t, root, "tag", "tools/other/v8.0.0")
	gitRun(t, root, "tag", "tools/widget/v1.4.2")
	if err := os.Remove(filepath.Join(moduleDir, "version.go")); err != nil {
		t.Fatal(err)
	}

	got, err := readCurrentVersionForModule(filepath.Join(moduleDir, "version.go"), moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.4.2" {
		t.Fatalf("discovered version = %q; want 1.4.2", got)
	}
}

func TestNestedModuleMajorMigration(t *testing.T) {
	root, moduleDir := initNestedModuleRepo(t, "example.com/acme/repo/tools/widget", "1.9.0")
	consumer := filepath.Join(moduleDir, "consumer.go")
	if err := os.WriteFile(consumer, []byte("package widget\n\nimport _ \"example.com/acme/repo/tools/widget/internal/value\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "add consumer")

	meta, err := RunWithOptions(VersionOptions{WorkDir: moduleDir}, "major")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Tag != "tools/widget/v2.0.0" {
		t.Fatalf("Tag = %q; want tools/widget/v2.0.0", meta.Tag)
	}
	goMod, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goMod), "module example.com/acme/repo/tools/widget/v2") {
		t.Fatalf("go.mod was not migrated: %s", goMod)
	}
	updated, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "example.com/acme/repo/tools/widget/v2/internal/value") {
		t.Fatalf("self import was not migrated: %s", updated)
	}
}

func initNestedModuleRepo(t *testing.T, modulePath, version string) (string, string) {
	t.Helper()
	root := t.TempDir()
	moduleDir := filepath.Join(root, "tools", "widget")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), fmt.Appendf(nil, "module %s\n\ngo 1.23\n", modulePath), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeVersionFile(filepath.Join(moduleDir, "version.go"), version); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "init")
	gitRun(t, root, "config", "user.email", "test@example.com")
	gitRun(t, root, "config", "user.name", "Test User")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "initial")
	return root, moduleDir
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
