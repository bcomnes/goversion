package goversion

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRewriteDocumentationText(t *testing.T) {
	for _, migration := range []struct{ name, oldMod, newMod string }{
		{"v1 to v2", "github.com/acme/foo", "github.com/acme/foo/v2"},
		{"v2 to v3", "github.com/acme/foo/v2", "github.com/acme/foo/v3"},
	} {
		t.Run(migration.name, func(t *testing.T) {
			old, next := migration.oldMod, migration.newMod
			for _, tc := range []struct{ name, input, want string }{
				{"bare module", old, next},
				{"bold module", "**" + old + "**", "**" + next + "**"},
				{"italic package", "*" + old + "/client*", "*" + next + "/client*"},
				{"bold docs URL", "**https://pkg.go.dev/" + old + "#Overview**", "**https://pkg.go.dev/" + next + "#Overview**"},
				{"italic docs URL", "*https://godoc.org/" + old + "/client*", "*https://godoc.org/" + next + "/client*"},
				{"compact module table", "|" + old + "|" + old + "/client|", "|" + next + "|" + next + "/client|"},
				{"compact URL table", "|https://pkg.go.dev/" + old + "|", "|https://pkg.go.dev/" + next + "|"},
				{"latest module", "go get " + old + "@latest", "go get " + next + "@latest"},
				{"latest godoc", "https://godoc.org/" + old + "@latest/client?tab=doc#Client", "https://godoc.org/" + next + "@latest/client?tab=doc#Client"},
				{"commands", "go get " + old + "\ngo install " + old + "/cmd/foo\n", "go get " + next + "\ngo install " + next + "/cmd/foo\n"},
				{"example import", "```go\nimport \"" + old + "/client\"\n```", "```go\nimport \"" + next + "/client\"\n```"},
				{"punctuation", "Use (`" + old + "`), \"" + old + "/client\".", "Use (`" + next + "`), \"" + next + "/client\"."},
				{"package docs", "[API](https://pkg.go.dev/" + old + "/client?tab=doc#Client)", "[API](https://pkg.go.dev/" + next + "/client?tab=doc#Client)"},
				{"module docs", "https://pkg.go.dev/" + old + "#section-readme", "https://pkg.go.dev/" + next + "#section-readme"},
				{"badge", "https://pkg.go.dev/badge/" + old, "https://pkg.go.dev/badge/" + next},
				{"svg badge", "![docs](https://pkg.go.dev/badge/" + old + ".svg?status=ok#badge)", "![docs](https://pkg.go.dev/badge/" + next + ".svg?status=ok#badge)"},
				{"subpackage badge", "https://pkg.go.dev/badge/" + old + "/client.svg", "https://pkg.go.dev/badge/" + next + "/client.svg"},
				{"godoc", "https://godoc.org/" + old + "/client?status.svg#Client", "https://godoc.org/" + next + "/client?status.svg#Client"},
				{"mixed references", old + " https://github.com/acme/foo " + old + "/client", next + " https://github.com/acme/foo " + next + "/client"},
				{"latest command", "go install " + old + "/cmd/foo@latest", "go install " + next + "/cmd/foo@latest"},
				{"latest package docs", "https://pkg.go.dev/" + old + "@latest/client#Client", "https://pkg.go.dev/" + next + "@latest/client#Client"},
				{"latest package suffix", "https://pkg.go.dev/" + old + "/client@latest#Client", "https://pkg.go.dev/" + next + "/client@latest#Client"},
				{"latest badge", "https://pkg.go.dev/badge/" + old + "@latest.svg", "https://pkg.go.dev/badge/" + next + "@latest.svg"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if got := rewriteDocumentationText(tc.input, old, next); got != tc.want {
						t.Errorf("rewriteDocumentationText(%q) = %q; want %q", tc.input, got, tc.want)
					}
				})
			}
			for _, input := range []string{
				"", "unrelated documentation", old + "-tools", old + "bar", "not" + old,
				"go get " + old + "@v1.2.3", "go install " + old + "/cmd/foo@v1.2.3",
				"import \"" + old + "@v1.2.3/client\"",
				"https://pkg.go.dev/" + old + "@v1.2.3/client?tab=doc#Client",
				"https://pkg.go.dev/" + old + "/client@v1.2.3#Client",
				"https://pkg.go.dev/badge/" + old + "@v1.2.3.svg",
				"https://godoc.org/" + old + "@v1.2.3/client#Client",
				"https://pkg.go.dev/" + old + "-tools/client",
				"https://github.com/acme/foo", "https://github.com/acme/foo/tree/main/client",
				"https://" + old + "/client", "https://example.org/" + old,
				"https://example.org/?module=" + old,
				"https://example.org/?module=(" + old + ")",
				"https://example.org/a(b)?module=" + old,
				"[other](https://example.org/?module=(" + old + "/client)&next=ok)",
				"https://example.org/?module=((" + old + "))#section",
			} {
				t.Run("preserve "+input, func(t *testing.T) {
					if got := rewriteDocumentationText(input, old, next); got != input {
						t.Errorf("rewriteDocumentationText(%q) = %q; want unchanged", input, got)
					}
				})
			}
		})
	}
}

func TestRewriteDocumentationTextPreservesOtherMajors(t *testing.T) {
	const old = "github.com/acme/foo"
	for _, suffix := range []string{"/v2", "/v3/client", "/v20/client", "/v2@latest", "/v3@latest/client"} {
		for _, prefix := range []string{"", "https://pkg.go.dev/", "https://pkg.go.dev/badge/", "https://godoc.org/"} {
			input := prefix + old + suffix
			if got := rewriteDocumentationText(input, old, old+"/v2"); got != input {
				t.Errorf("rewriteDocumentationText(%q) = %q; want unchanged", input, got)
			}
		}
	}
	input := old + " https://pkg.go.dev/" + old + "/client#Client"
	if got := rewriteDocumentationText(input, old, old); got != input {
		t.Errorf("same-module rewrite = %q; want %q", got, input)
	}
}

func TestUpdateDocumentationReferences(t *testing.T) {
	const old = "github.com/acme/foo"
	const next = old + "/v2"
	dir := t.TempDir()
	before := map[string]string{
		"go.mod":                      "module " + old + "\n\ngo 1.23\n",
		"README.md":                   "go get " + old + "\n",
		"docs/guide.MD":               "import \"" + old + "/client\"\n",
		"docs/api.MarkDown":           "https://pkg.go.dev/" + old + "#Overview\n",
		"docs/usage.markdown":         "https://godoc.org/" + old + "/client\n",
		"doc.go":                      "// Package foo uses " + old + ".\npackage foo\n\n/* See https://pkg.go.dev/" + old + "/client#Client. */\nconst URL = \"" + old + "\"\nconst Raw = `https://pkg.go.dev/" + old + "`\nvar _ = \"https://example.org\" // " + old + "/client\n",
		"literal.go":                  "package foo\nconst Text = \"// " + old + "\"\nconst Block = `/* " + old + " */`\n",
		"imports.go":                  "package foo\nimport _ \"" + old + "/client\"\n",
		"notes.txt":                   old,
		"unchanged.md":                old + "@v1.2.3\n",
		".git/hidden.md":              old,
		"vendor/dependency/README.md": old,
		"nested/go.mod":               "module example.com/nested\n\ngo 1.23\n",
		"nested/README.md":            old,
		"nested/deep/doc.go":          "package nested // " + old + "\n",
	}
	after := make(map[string]string, len(before))
	for name, content := range before {
		documentationWrite(t, filepath.Join(dir, name), content)
		after[name] = content
	}
	for _, name := range []string{"README.md", "docs/guide.MD", "docs/api.MarkDown", "docs/usage.markdown"} {
		after[name] = strings.ReplaceAll(before[name], old, next)
	}
	after["doc.go"] = "// Package foo uses " + next + ".\npackage foo\n\n/* See https://pkg.go.dev/" + next + "/client#Client. */\nconst URL = \"" + old + "\"\nconst Raw = `https://pkg.go.dev/" + old + "`\nvar _ = \"https://example.org\" // " + next + "/client\n"
	readme := filepath.Join(dir, "README.md")
	if err := os.Chmod(readme, 0o640); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	external := filepath.Join(outside, "README.md")
	documentationWrite(t, external, old)
	for name, target := range map[string]string{"linked.md": external, "linked-dir": outside, "internal-link.md": readme, "broken.md": filepath.Join(outside, "missing")} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{readme, filepath.Join(dir, "docs/guide.MD"), filepath.Join(dir, "docs/api.MarkDown"), filepath.Join(dir, "docs/usage.markdown"), filepath.Join(dir, "doc.go")}
	for _, dry := range []bool{true, false} {
		got, err := updateDocumentationReferences(dir, old, next, dry)
		if err != nil {
			t.Fatalf("dryRun=%v: %v", dry, err)
		}
		documentationAssertPaths(t, got, want)
		expected := after
		if dry {
			expected = before
		}
		for name, content := range expected {
			documentationAssertContent(t, filepath.Join(dir, name), content)
		}
		documentationAssertContent(t, external, old)
		info, err := os.Stat(readme)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Errorf("dryRun=%v: mode = %o; want 640", dry, info.Mode().Perm())
		}
		for _, name := range []string{"linked.md", "linked-dir", "internal-link.md", "broken.md"} {
			info, err := os.Lstat(filepath.Join(dir, name))
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("symlink %s was changed: %v", name, err)
			}
		}
	}
	for _, dry := range []bool{true, false} {
		got, err := updateDocumentationReferences(dir, next, next, dry)
		if err != nil || len(got) != 0 {
			t.Errorf("same module dryRun=%v: files=%v err=%v; want no-op", dry, got, err)
		}
		for name, content := range after {
			documentationAssertContent(t, filepath.Join(dir, name), content)
		}
	}
}

func TestUpdateDocumentationReferencesPreservesCommentBytes(t *testing.T) {
	const old = "github.com/acme/foo"
	const next = old + "/v2"
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.go")
	// Deliberately retain CRLF, non-gofmt spacing, Unicode, and a final comment without a newline.
	before := "// Package foo: café " + old + "\r\npackage foo\r\n\r\n" +
		"/* first line\r\n\t" + old + "/client\r\n last line */\r\n" +
		"var  Literal = \"" + old + "\"  // " + old + "@latest\r\n" +
		"var Raw = `/* " + old + " */`\r\n" +
		"// https://pkg.go.dev/" + old + "#Overview"
	want := "// Package foo: café " + next + "\r\npackage foo\r\n\r\n" +
		"/* first line\r\n\t" + next + "/client\r\n last line */\r\n" +
		"var  Literal = \"" + old + "\"  // " + next + "@latest\r\n" +
		"var Raw = `/* " + old + " */`\r\n" +
		"// https://pkg.go.dev/" + next + "#Overview"
	documentationWrite(t, path, before)
	for _, dry := range []bool{true, false} {
		got, err := updateDocumentationReferences(dir, old, next, dry)
		if err != nil {
			t.Fatalf("dryRun=%v: %v", dry, err)
		}
		documentationAssertPaths(t, got, []string{path})
		expected := want
		if dry {
			expected = before
		}
		documentationAssertContent(t, path, expected)
	}
}

func TestDocumentationMajorIntegration(t *testing.T) {
	for _, tc := range []struct{ version, oldMod, newMod, tag string }{
		{"1.9.0", "example.com/acme/repo/tools/widget", "example.com/acme/repo/tools/widget/v2", "tools/widget/v2.0.0"},
		{"2.9.0", "example.com/acme/repo/tools/widget/v2", "example.com/acme/repo/tools/widget/v3", "tools/widget/v3.0.0"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			root, dir := initNestedModuleRepo(t, tc.oldMod, tc.version)
			readme := filepath.Join(dir, "README.md")
			consumer := filepath.Join(dir, "consumer.go")
			comment := filepath.Join(dir, "doc.go")
			testFile := filepath.Join(dir, "consumer_test.go")
			trackedIgnored := filepath.Join(dir, "generated", "tracked.md")
			ignored := filepath.Join(dir, "generated", "untracked.md")
			original := map[string]string{
				readme:         "go get " + tc.oldMod + "\n",
				testFile:       "package widget_test\n\nimport _ \"" + tc.oldMod + "/client\"\n\n// See https://pkg.go.dev/" + tc.oldMod + "/client.\nconst RuntimePath = \"" + tc.oldMod + "\"\n",
				trackedIgnored: "go get " + tc.oldMod + "@latest\n",
				consumer:       "package widget\n\nimport _ \"" + tc.oldMod + "/client\"\n\n// See https://pkg.go.dev/" + tc.oldMod + "/client.\nconst RuntimePath = \"" + tc.oldMod + "\"\n",
				comment:        "// Package widget uses " + tc.oldMod + ".\npackage widget\n",
			}
			for path, content := range original {
				documentationWrite(t, path, content)
			}
			outside := filepath.Join(root, "README.md")
			documentationWrite(t, outside, tc.oldMod)
			documentationWrite(t, filepath.Join(dir, ".gitignore"), "generated/\n")
			documentationWrite(t, ignored, "go get "+tc.oldMod+"\n")
			gitRun(t, root, "add", ".")
			gitRun(t, root, "add", "-f", trackedIgnored)
			gitRun(t, root, "commit", "-m", "add documentation fixtures")
			head := gitOutput(t, root, "--no-pager", "rev-parse", "HEAD")
			options := VersionOptions{WorkDir: dir}
			dry, err := DryRunWithOptions(options, "major")
			if err != nil {
				t.Fatal(err)
			}
			want := []string{filepath.Join(dir, "go.mod"), filepath.Join(dir, "version.go"), readme, consumer, comment, testFile, trackedIgnored}
			documentationAssertPaths(t, dry.UpdatedFiles, want)
			for path, content := range original {
				documentationAssertContent(t, path, content)
			}
			documentationAssertContent(t, ignored, "go get "+tc.oldMod+"\n")
			if got := gitOutput(t, root, "--no-pager", "rev-parse", "HEAD"); got != head {
				t.Error("dry run changed HEAD")
			}
			if got := gitOutput(t, root, "--no-pager", "tag", "--list"); got != "" {
				t.Errorf("dry run created tags: %s", got)
			}
			if got := gitOutput(t, root, "--no-pager", "status", "--porcelain"); got != "" {
				t.Fatalf("dry run changed worktree/index: %s", got)
			}
			wet, err := RunWithOptions(options, "major")
			if err != nil {
				t.Fatal(err)
			}
			documentationAssertPaths(t, wet.UpdatedFiles, want)
			if dry.Tag != tc.tag || wet.Tag != tc.tag || dry.NewVersion != wet.NewVersion {
				t.Errorf("unexpected metadata: dry=%+v wet=%+v", dry, wet)
			}
			documentationAssertContent(t, readme, strings.ReplaceAll(original[readme], tc.oldMod, tc.newMod))
			documentationAssertContent(t, comment, strings.ReplaceAll(original[comment], tc.oldMod, tc.newMod))
			documentationAssertContent(t, trackedIgnored, strings.ReplaceAll(original[trackedIgnored], tc.oldMod, tc.newMod))
			for _, path := range []string{consumer, testFile} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, text := range []string{"import _ \"" + tc.newMod + "/client\"", "// See https://pkg.go.dev/" + tc.newMod + "/client.", "const RuntimePath = \"" + tc.oldMod + "\""} {
					if !strings.Contains(string(data), text) {
						t.Errorf("%s missing %q:\n%s", path, text, data)
					}
				}
			}
			documentationAssertContent(t, outside, tc.oldMod)
			documentationAssertContent(t, ignored, "go get "+tc.oldMod+"\n")
			if got := gitOutput(t, root, "--no-pager", "status", "--porcelain"); got != "" {
				t.Errorf("run left unstaged/uncommitted changes: %s", got)
			}
			if got := gitOutput(t, root, "--no-pager", "ls-files", "--", ignored); got != "" {
				t.Errorf("ignored Markdown was added to Git: %s", got)
			}
			for _, path := range []string{readme, consumer, comment, testFile, trackedIgnored} {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					t.Fatal(err)
				}
				committed := gitOutput(t, root, "--no-pager", "show", tc.tag+":"+filepath.ToSlash(rel))
				documentationAssertContent(t, path, committed)
			}
		})
	}
}

func TestDocumentationNonMigratingBumps(t *testing.T) {
	for _, tc := range []struct{ version, bump string }{
		{"1.9.0", "patch"}, {"1.9.0", "minor"}, {"1.9.0", "premajor"}, {"1.9.0", "2.0.0"}, {"0.9.0", "major"},
	} {
		t.Run(tc.version+"/"+tc.bump, func(t *testing.T) {
			const mod = "example.com/acme/repo/tools/widget"
			root, dir := initNestedModuleRepo(t, mod, tc.version)
			path := filepath.Join(dir, "README.md")
			documentationWrite(t, path, "go get "+mod+"\n")
			gitRun(t, root, "add", ".")
			gitRun(t, root, "commit", "-m", "add documentation fixture")
			for _, run := range []func(VersionOptions, string) (VersionMeta, error){DryRunWithOptions, RunWithOptions} {
				meta, err := run(VersionOptions{WorkDir: dir}, tc.bump)
				if err != nil {
					t.Fatal(err)
				}
				if containsPath(meta.UpdatedFiles, path) {
					t.Errorf("non-migrating bump reports documentation: %v", meta.UpdatedFiles)
				}
				documentationAssertContent(t, path, "go get "+mod+"\n")
				data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(data), "module "+mod+"\n") {
					t.Errorf("non-migrating bump changed module path: %s", data)
				}
			}
		})
	}
}

func documentationWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func documentationAssertContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s contents = %q; want %q", path, got, want)
	}
}

func documentationAssertPaths(t *testing.T, got, want []string) {
	t.Helper()
	got = slices.Clone(got)
	want = slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("updated files = %v; want %v (each exactly once)", got, want)
	}
}
