package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestVersionCommandRelativeFileLists(t *testing.T) {
	for _, workdir := range []string{"default", "relative", "absolute"} {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", workdir, dry), func(t *testing.T) {
				root := t.TempDir()
				moduleDir := filepath.Join(root, "tools", "widget")
				before := map[string]string{
					"tools/widget/go.mod":          "module example.com/acme/repo/tools/widget\n\ngo 1.23\n",
					"tools/widget/version.go":      "package widget\n\nvar Version = \"1.2.3\"\n",
					"tools/widget/docs/version.md": "Version 1.2.3\n",
					"docs/widget-install.md":       "Install version 1.2.3\n",
					"docs/extra.md":                "Release 1.2.3\n",
				}
				for name, content := range before {
					path := filepath.Join(root, filepath.FromSlash(name))
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				for _, args := range [][]string{
					{"init"}, {"config", "user.email", "test@example.com"},
					{"config", "user.name", "Test User"}, {"config", "commit.gpgsign", "false"},
					{"config", "tag.gpgsign", "false"}, {"add", "."}, {"commit", "-m", "initial"},
				} {
					versionSkipDocsGit(t, root, args...)
				}
				head := versionSkipDocsGit(t, root, "rev-parse", "HEAD")
				var args []string
				switch workdir {
				case "default":
					t.Chdir(moduleDir)
				case "relative":
					t.Chdir(root)
					args = append(args, "-workdir", filepath.Join("tools", "widget"))
				case "absolute":
					t.Chdir(root)
					args = append(args, "-workdir", moduleDir)
				}
				args = append(args, "-bump-file", "docs/version.md", "-bump-file", "../../docs/widget-install.md",
					"-bump-file", filepath.Join(root, "docs", "extra.md"))
				heading := "Files updated:\n"
				if dry {
					args = append(args, "-dry")
					heading = "Files that would be updated:\n"
				}
				args = append(args, "major")
				var output, errorOutput bytes.Buffer
				if code := runVersionCommand(args, &output, &errorOutput); code != 0 {
					t.Fatalf("runVersionCommand returned %d: %s", code, errorOutput.String())
				}
				_, files, found := strings.Cut(output.String(), heading)
				if !found {
					t.Fatalf("output missing %q:\n%s", heading, output.String())
				}
				listed := strings.Split(strings.TrimSuffix(files, "\n"), "\n")
				want := []string{"version.go", "go.mod", "docs/version.md", "../../docs/widget-install.md", "../../docs/extra.md"}
				for i, path := range want {
					want[i] = "  " + filepath.FromSlash(path)
				}
				slices.Sort(listed)
				slices.Sort(want)
				if !slices.Equal(listed, want) {
					t.Errorf("file list = %q; want %q", listed, want)
				}
				if dry {
					for name, content := range before {
						got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
						if err != nil {
							t.Fatal(err)
						}
						if string(got) != content {
							t.Errorf("dry run changed %s", name)
						}
					}
					if got := versionSkipDocsGit(t, root, "rev-parse", "HEAD"); got != head {
						t.Errorf("dry run changed HEAD: %s -> %s", head, got)
					}
				}
				if status := versionSkipDocsGit(t, root, "status", "--porcelain"); status != "" {
					t.Errorf("working tree is dirty: %s", status)
				}
			})
		}
	}
}

func TestVersionCommandWorkdirDryRun(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "go")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/acme/repo/go\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "version.go"), []byte("package tool\n\nvar Version = \"1.2.3\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test User"}, {"add", "."}, {"commit", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}

	var output, errorOutput bytes.Buffer
	code := runVersionCommand([]string{"-workdir", moduleDir, "-dry", "patch"}, &output, &errorOutput)
	if code != 0 {
		t.Fatalf("runVersionCommand returned %d: %s", code, errorOutput.String())
	}
	for _, want := range []string{"New Version: 1.2.4", "Tag:         go/v1.2.4"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
	contents, err := os.ReadFile(filepath.Join(moduleDir, "version.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), fmt.Sprintf("%q", "1.2.3")) {
		t.Fatalf("dry run changed version.go: %s", contents)
	}
}
