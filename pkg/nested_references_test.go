package goversion

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nestedReferencesParent = "example.com/repo"

var nestedReferencesCases = []struct {
	name, directory, module string
}{
	{"matching directory", "tools", "example.com/repo/tools"},
	{"declared path differs from directory", "internal/buildkit", "example.com/repo/tools"},
	{"versioned child family", "tools", "example.com/repo/tools/v2"},
}

// Exercise discovery through the scan/update APIs rather than supplying an
// exclusion list: the boundary's module declaration must determine ownership.
func TestNestedReferencesDocumentation(t *testing.T) {
	for _, tc := range nestedReferencesCases {
		t.Run(tc.name, func(t *testing.T) {
			dir, before, after := nestedReferencesFixture(t, tc.directory, tc.module)
			want := nestedReferencesPaths(dir, "mixed.md", "doc.go")
			for _, dry := range []bool{true, false} {
				got, err := updateDocumentationReferences(dir, nestedReferencesParent, nestedReferencesParent+"/v2", dry)
				if err != nil {
					t.Fatal(err)
				}
				documentationAssertPaths(t, got, want)
				for name, content := range before {
					if !dry && (name == "mixed.md" || name == "doc.go") {
						content = after[name]
					}
					documentationAssertContent(t, filepath.Join(dir, name), content)
				}
			}
		})
	}
}

func TestNestedReferencesImports(t *testing.T) {
	for _, tc := range nestedReferencesCases {
		t.Run(tc.name, func(t *testing.T) {
			dir, before, after := nestedReferencesFixture(t, tc.directory, tc.module)
			want := nestedReferencesPaths(dir, "consumer.go", "consumer_test.go")
			got, err := scanSelfImports(dir, nestedReferencesParent, nestedReferencesParent+"/v2")
			if err != nil {
				t.Fatal(err)
			}
			documentationAssertPaths(t, got, want)
			for name, content := range before {
				documentationAssertContent(t, filepath.Join(dir, name), content)
			}
			got, err = updateSelfImports(dir, nestedReferencesParent, nestedReferencesParent+"/v2")
			if err != nil {
				t.Fatal(err)
			}
			documentationAssertPaths(t, got, want)
			for name, content := range before {
				if name == "consumer.go" || name == "consumer_test.go" {
					nestedReferencesAssertGo(t, filepath.Join(dir, name), after[name])
				} else {
					documentationAssertContent(t, filepath.Join(dir, name), content)
				}
			}

		})
	}
}

func TestNestedReferencesMajorIntegration(t *testing.T) {
	for _, tc := range nestedReferencesCases {
		t.Run(tc.name, func(t *testing.T) {
			dir, before, after := nestedReferencesFixture(t, tc.directory, tc.module)
			gitRun(t, dir, "init")
			gitRun(t, dir, "config", "user.email", "test@example.com")
			gitRun(t, dir, "config", "user.name", "Test User")
			gitRun(t, dir, "add", ".")
			gitRun(t, dir, "commit", "-m", "nested reference fixture")
			for _, name := range []string{"go.mod", "README.md", "child.go", "child_test.go"} {
				path := filepath.ToSlash(filepath.Join(tc.directory, name))
				if got := gitOutput(t, dir, "--no-pager", "ls-files", "--", path); strings.TrimSpace(got) != path {
					t.Fatalf("nested fixture is not tracked: %s (%q)", path, got)
				}
			}
			head := gitOutput(t, dir, "--no-pager", "rev-parse", "HEAD")
			want := nestedReferencesPaths(dir, "go.mod", "version.go", "mixed.md", "doc.go", "consumer.go", "consumer_test.go")
			options := VersionOptions{WorkDir: dir}
			dry, err := DryRunWithOptions(options, "major")
			if err != nil {
				t.Fatal(err)
			}
			documentationAssertPaths(t, dry.UpdatedFiles, want)
			for name, content := range before {
				documentationAssertContent(t, filepath.Join(dir, name), content)
			}
			if got := gitOutput(t, dir, "--no-pager", "rev-parse", "HEAD"); got != head {
				t.Error("DryRun changed HEAD")
			}
			if got := gitOutput(t, dir, "--no-pager", "tag", "--list"); got != "" {
				t.Errorf("DryRun created tags: %s", got)
			}
			if got := gitOutput(t, dir, "--no-pager", "status", "--porcelain"); got != "" {
				t.Errorf("DryRun changed worktree/index: %s", got)
			}
			wet, err := RunWithOptions(options, "major")
			if err != nil {
				t.Fatal(err)
			}
			documentationAssertPaths(t, wet.UpdatedFiles, want)
			if dry.Tag != "v2.0.0" || wet.Tag != dry.Tag || wet.NewVersion != dry.NewVersion {
				t.Errorf("unexpected metadata: dry=%+v wet=%+v", dry, wet)
			}
			for name, content := range after {
				path := filepath.Join(dir, name)
				if name == "consumer.go" || name == "consumer_test.go" {
					nestedReferencesAssertGo(t, path, content)
				} else if name == "version.go" {
					version, err := readCurrentVersion(path)
					if err != nil || normalizeVersion(version) != "v2.0.0" {
						t.Errorf("version = %q, err = %v; want v2.0.0", version, err)
					}
				} else {
					documentationAssertContent(t, path, content)
				}
				committed := gitOutput(t, dir, "--no-pager", "show", wet.Tag+":"+filepath.ToSlash(name))
				documentationAssertContent(t, path, committed)
			}
			if got := gitOutput(t, dir, "--no-pager", "status", "--porcelain"); got != "" {
				t.Errorf("Run left worktree/index dirty: %s", got)
			}
		})
	}
}

func nestedReferencesFixture(t *testing.T, childDir, childModule string) (string, map[string]string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	const old = nestedReferencesParent
	const next = old + "/v2"
	// A /v2 declaration protects the entire child family, not just /v2.
	var protected strings.Builder
	for _, module := range []string{old + "/tools", old + "/tools/v2", old + "/tools/v3", old + "/tools/v20", old + "-tools"} {
		protected.WriteString(nestedReferencesDocumentation(module))
	}
	own := nestedReferencesDocumentation(old)
	updatedOwn := nestedReferencesDocumentation(next)
	// Similar package names are still parent-owned, not part of the child family.
	own += old + "/toolshed/pkg\n" + old + "/tools-extra/pkg\n"
	updatedOwn += next + "/toolshed/pkg\n" + next + "/tools-extra/pkg\n"
	// The physical directory is not the module's import path.
	if childDir != "tools" {
		own += old + "/" + filepath.ToSlash(childDir) + "/pkg\n"
		updatedOwn += next + "/" + filepath.ToSlash(childDir) + "/pkg\n"
	}
	runtime := "const RuntimePath = \"" + old + "/pkg\"\nconst RuntimeDocs = `https://pkg.go.dev/" + old + "#Overview`\n"
	doc := "// Package repo references " + old + " and " + old + "/tools.\npackage repo\n\n/*\n" + protected.String() + own + "*/\n" + runtime
	updatedDoc := "// Package repo references " + next + " and " + old + "/tools.\npackage repo\n\n/*\n" + protected.String() + updatedOwn + "*/\n" + runtime
	protectedImports := ""
	for _, path := range []string{old + "/tools", old + "/tools/pkg", old + "/tools/v2/pkg", old + "/tools/v3/pkg", old + "/tools/v20/pkg", old + "-tools/pkg"} {
		protectedImports += "import _ \"" + path + "\"\n"
	}
	before := map[string]string{
		"go.mod":                                 "module " + old + "\n\ngo 1.23\n",
		"version.go":                             "package repo\n\nvar Version = \"1.9.0\"\n",
		"README.md":                              protected.String(),
		"mixed.md":                               protected.String() + own,
		"doc.go":                                 doc,
		"protected.go":                           "package repo\n\n" + protectedImports,
		"protected_test.go":                      "package repo_test\n\n" + protectedImports,
		"consumer.go":                            "package repo\n\n" + protectedImports + "import _ \"" + old + "\"\nimport _ \"" + old + "/pkg\"\nimport _ \"" + old + "/toolshed/pkg\"\n\n" + runtime,
		"consumer_test.go":                       "package repo_test\n\n" + protectedImports + "import _ \"" + old + "/pkg\"\n\n" + runtime,
		filepath.Join(childDir, "go.mod"):        "module " + childModule + "\n\ngo 1.23\n",
		filepath.Join(childDir, "README.md"):     own + protected.String(),
		filepath.Join(childDir, "child.go"):      "// Package tools uses " + old + "/pkg.\npackage tools\n\nimport _ \"" + old + "/pkg\"\n",
		filepath.Join(childDir, "child_test.go"): "package tools_test\n\nimport _ \"" + old + "/pkg\"\n",
	}
	after := make(map[string]string, len(before))
	for name, content := range before {
		documentationWrite(t, filepath.Join(dir, name), content)
		after[name] = content
	}
	after["go.mod"] = "module " + next + "\n\ngo 1.23\n"
	after["mixed.md"] = protected.String() + updatedOwn
	after["doc.go"] = updatedDoc
	for _, name := range []string{"consumer.go", "consumer_test.go"} {
		content := before[name]
		for _, suffix := range []string{"", "/pkg", "/toolshed/pkg"} {
			content = strings.ReplaceAll(content, "import _ \""+old+suffix+"\"", "import _ \""+next+suffix+"\"")
		}
		after[name] = content
	}
	return dir, before, after
}

func nestedReferencesDocumentation(module string) string {
	return strings.Join([]string{
		module, module + "/pkg", "go get " + module + "@latest",
		"go install " + module + "/cmd/tool@latest",
		"[API](https://pkg.go.dev/" + module + "#Overview)",
		"https://pkg.go.dev/" + module + "@latest/pkg?tab=doc#Symbol",
		"https://pkg.go.dev/" + module + "/pkg@latest#Symbol",
		"https://godoc.org/" + module + "/pkg?status.svg#Symbol",
		"https://godoc.org/" + module + "@latest/pkg#Symbol",
		"![docs](https://pkg.go.dev/badge/" + module + ".svg?status=ok#badge)",
		"https://pkg.go.dev/badge/" + module + "@latest.svg#badge",
		"https://pkg.go.dev/badge/" + module + "/pkg.svg#badge",
	}, "\n") + "\n"
}

func nestedReferencesPaths(dir string, names ...string) []string {
	paths := make([]string, len(names))
	for i, name := range names {
		paths[i] = filepath.Join(dir, name)
	}
	return paths
}

func nestedReferencesAssertGo(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := format.Source(data)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := format.Source([]byte(want))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(expected) {
		t.Errorf("%s contents after gofmt:\n%s\nwant:\n%s", path, got, expected)
	}
}
