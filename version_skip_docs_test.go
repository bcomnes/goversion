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

func TestVersionSkipDocsHelp(t *testing.T) {
	out, err := runCLI([]string{"-help"})
	if err != nil {
		t.Fatalf("help failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "-skip-docs") {
		t.Errorf("help missing -skip-docs:\n%s", out)
	}
}

func TestVersionSkipDocsWorkdir(t *testing.T) {
	runners := []struct {
		name string
		run  func([]string) (string, error)
	}{
		{"runCLI", func(args []string) (string, error) { return runCLI(args) }},
		{"runVersionCommand", func(args []string) (string, error) {
			var output, errorOutput bytes.Buffer
			if code := runVersionCommand(args, &output, &errorOutput); code != 0 {
				return output.String(), fmt.Errorf("exit code %d: %s", code, errorOutput.String())
			}
			return output.String(), nil
		}},
	}
	for _, runner := range runners {
		t.Run(runner.name, func(t *testing.T) {
			for _, tc := range []struct {
				name     string
				skipDocs bool
				dry      bool
			}{
				{"skipDocsDry", true, true},
				{"skipDocsReal", true, false},
				{"defaultUpdatesDocs", false, false},
				{"defaultUpdatesDocsDry", false, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					dir, before := newVersionSkipDocsFixture(t)
					head := versionSkipDocsGit(t, dir, "rev-parse", "HEAD")
					args := []string{"-workdir", dir}
					if tc.skipDocs {
						args = append(args, "-skip-docs")
					}
					if tc.dry {
						args = append(args, "-dry")
					}
					args = append(args, "major")
					out, err := runner.run(args)
					if err != nil {
						t.Fatalf("major bump failed: %v\n%s", err, out)
					}
					for _, want := range []string{"Old Version: 1.2.3", "New Version: 2.0.0", "Tag:         v2.0.0"} {
						if !strings.Contains(out, want) {
							t.Errorf("output missing %q:\n%s", want, out)
						}
					}
					heading := "Files updated:\n"
					if tc.dry {
						heading = "Files that would be updated:\n"
					}
					_, files, found := strings.Cut(out, heading)
					if !found {
						t.Fatalf("output missing %q:\n%s", heading, out)
					}
					var listed, expected []string
					for _, line := range strings.Split(strings.TrimSpace(files), "\n") {
						listed = append(listed, strings.TrimSpace(line))
					}
					for _, name := range []string{"version.go", "go.mod", "consumer.go", "README.md", "doc.go"} {
						if !tc.skipDocs || (name != "README.md" && name != "doc.go") {
							expected = append(expected, name)
						}
					}
					slices.Sort(listed)
					slices.Sort(expected)
					if !slices.Equal(listed, expected) {
						t.Errorf("updated file list:\ngot: %q\nwant: %q\n%s", listed, expected, out)
					}
					for name, original := range before {
						want := original
						if !tc.dry {
							switch name {
							case "version.go":
								want = strings.ReplaceAll(original, "1.2.3", "2.0.0")
							case "go.mod":
								want = strings.ReplaceAll(original, versionSkipDocsModule, versionSkipDocsModule+"/v2")
							case "consumer.go":
								want = strings.Replace(original, `import _ "`+versionSkipDocsModule+`/client"`, `import _ "`+versionSkipDocsModule+`/v2/client"`, 1)
							}
							if !tc.skipDocs && (name == "README.md" || name == "doc.go" || name == "consumer.go") {
								want = strings.ReplaceAll(original, versionSkipDocsModule, versionSkipDocsModule+"/v2")
							}
						}
						got, err := os.ReadFile(filepath.Join(dir, name))
						if err != nil {
							t.Fatal(err)
						}
						if string(got) != want {
							t.Errorf("%s contents:\ngot:\n%s\nwant:\n%s", name, got, want)
						}
					}
					if status := versionSkipDocsGit(t, dir, "status", "--porcelain"); status != "" {
						t.Errorf("working tree is dirty: %s", status)
					}
					if tc.dry {
						if got := versionSkipDocsGit(t, dir, "rev-parse", "HEAD"); got != head {
							t.Errorf("dry run changed HEAD: %s -> %s", head, got)
						}
						if tags := versionSkipDocsGit(t, dir, "tag", "--list"); tags != "" {
							t.Errorf("dry run created tags: %s", tags)
						}
					} else {
						if tag := versionSkipDocsGit(t, dir, "rev-parse", "v2.0.0^{commit}"); tag != versionSkipDocsGit(t, dir, "rev-parse", "HEAD") || tag == head {
							t.Errorf("version tag does not identify a new HEAD commit: %s", tag)
						}
					}
				})
			}
		})
	}
}

const versionSkipDocsModule = "example.com/acme/widget"

func newVersionSkipDocsFixture(t *testing.T) (string, map[string]string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	dir := t.TempDir()
	before := map[string]string{
		"go.mod":           "module " + versionSkipDocsModule + "\n\ngo 1.23\n",
		"version.go":       "package widget\n\nvar (\n\tVersion = \"1.2.3\"\n)\n",
		"README.md":        "# Widget\n\ngo get " + versionSkipDocsModule + "\n\nhttps://pkg.go.dev/" + versionSkipDocsModule + "#Overview\n",
		"doc.go":           "// Package widget uses " + versionSkipDocsModule + ".\npackage widget\n\n/* See https://pkg.go.dev/" + versionSkipDocsModule + "/client. */\n",
		"consumer.go":      "package widget\n\n// See https://pkg.go.dev/" + versionSkipDocsModule + "/client.\nimport _ \"" + versionSkipDocsModule + "/client\"\t// " + versionSkipDocsModule + "/client\n\n/* Install with go get " + versionSkipDocsModule + ". */\n",
		"client/client.go": "package client\n",
	}
	for name, content := range before {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	versionSkipDocsGit(t, dir, "init")
	versionSkipDocsGit(t, dir, "config", "user.email", "test@example.com")
	versionSkipDocsGit(t, dir, "config", "user.name", "Test User")
	versionSkipDocsGit(t, dir, "config", "commit.gpgsign", "false")
	versionSkipDocsGit(t, dir, "config", "tag.gpgsign", "false")
	versionSkipDocsGit(t, dir, "add", ".")
	versionSkipDocsGit(t, dir, "commit", "-m", "initial")
	return dir, before
}

func versionSkipDocsGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"--no-pager"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
