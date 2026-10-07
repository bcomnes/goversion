package goversion

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const moduleFilesOld = "example.com/acme/widget"
const moduleFilesNew = moduleFilesOld + "/v2"

type moduleFilesFixture struct {
	dir      string
	before   map[string]string
	eligible []string
	imports  []string
	docs     []string
	links    []string
}

func newModuleFilesFixture(t *testing.T, git, nested bool) moduleFilesFixture {
	t.Helper()
	root := t.TempDir()
	if git {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git is not available")
		}
		gitRun(t, root, "init")
	} else {
		cmd := exec.Command("git", "rev-parse", "--show-toplevel")
		cmd.Dir = root
		if err := cmd.Run(); err == nil {
			t.Fatal("filesystem fallback fixture unexpectedly belongs to a Git repository")
		}
	}
	dir := root
	if nested {
		dir = filepath.Join(root, "tools", "widget")
		documentationWrite(t, filepath.Join(root, "sibling.go"), "not Go source\n")
		documentationWrite(t, filepath.Join(root, "go.mod"), "module example.com/acme/repo\n")
	}
	f := moduleFilesFixture{dir: dir, before: make(map[string]string)}
	write := func(name, content string) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		documentationWrite(t, path, content)
		f.before[path] = content
	}
	write("go.mod", "module "+moduleFilesOld+"\n\ngo 1.23\n")
	f.eligible = append(f.eligible, filepath.Join(dir, "go.mod"))
	prefixes := []string{"src", "untracked/deep", "web/node_modules/package", "space and\nnewline"}
	if git {
		write(".gitignore", "/node_modules/\n/generated/*\n!/generated/keep/\n/artifacts/\n")
		f.eligible = append(f.eligible, filepath.Join(dir, ".gitignore"))
		prefixes = append(prefixes, "node_modules/tracked/deep", "generated/tracked/deep", "generated/keep/deep")
	} else {
		// Ignore rules have no effect outside Git, including for node_modules.
		write(".gitignore", "node_modules/\ngenerated/\n")
		f.eligible = append(f.eligible, filepath.Join(dir, ".gitignore"))
		prefixes = append(prefixes, "node_modules/package", "generated/deep")
	}
	for _, prefix := range prefixes {
		goName := prefix + "/consumer.go"
		mdName := prefix + "/README.markdown"
		write(goName, "// Package widget uses "+moduleFilesOld+".\npackage widget\n\nimport _ \""+moduleFilesOld+"/client\"\n")
		write(mdName, "go get "+moduleFilesOld+"/client\n")
		goPath, mdPath := filepath.Join(dir, goName), filepath.Join(dir, mdName)
		f.eligible = append(f.eligible, goPath, mdPath)
		f.imports = append(f.imports, goPath)
		f.docs = append(f.docs, goPath, mdPath)
	}
	write("notes.txt", moduleFilesOld)
	f.eligible = append(f.eligible, filepath.Join(dir, "notes.txt"))
	write("nested/go.mod", "module example.com/other\n")
	excluded := []string{"vendor/dependency", "nested/deep", ".git/walker-fixture", "deep/.git", "deep/vendor", "deep/nested"}
	write("deep/nested/go.mod", "module example.com/other/deep\n")
	if git {
		excluded = append(excluded, "node_modules/broken/deep", "generated/discard/deep", "artifacts/arbitrary/deep")
	}
	for _, prefix := range excluded {
		write(prefix+"/broken.go", "not valid Go source\n")
		write(prefix+"/consumer.go", "package excluded\nimport _ \""+moduleFilesOld+"/client\"\n")
		write(prefix+"/README.md", moduleFilesOld+"\n")
	}
	outside := t.TempDir()
	for name, content := range map[string]string{
		"consumer.go": "package external\nimport _ \"" + moduleFilesOld + "/client\"\n",
		"README.md":   moduleFilesOld + "\n",
	} {
		path := filepath.Join(outside, name)
		documentationWrite(t, path, content)
		f.before[path] = content
	}
	for name, target := range map[string]string{
		"linked.go":        filepath.Join(outside, "consumer.go"),
		"linked.md":        filepath.Join(outside, "README.md"),
		"linked-dir":       outside,
		"internal-link.go": filepath.Join(dir, "src", "consumer.go"),
		"broken-link.go":   filepath.Join(outside, "missing.go"),
	} {
		path := filepath.Join(dir, name)
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		f.links = append(f.links, path)
	}
	if git {
		// Index entries must remain eligible even though their directories are ignored.
		gitRun(t, dir, "add", "-f", "--", "go.mod", ".gitignore", "src", "node_modules/tracked", "generated/tracked", "vendor", "nested", "deep/vendor", "deep/nested", "linked.go", "linked.md", "linked-dir", "internal-link.go", "broken-link.go")
	}
	return f
}

func (f moduleFilesFixture) assertUnchanged(t *testing.T) {
	t.Helper()
	for path, content := range f.before {
		documentationAssertContent(t, path, content)
	}
}

func TestWalkModuleFilesEligibility(t *testing.T) {
	for _, tc := range []struct {
		name   string
		git    bool
		nested bool
	}{
		{"git root", true, false},
		{"module below git root", true, true},
		{"outside git", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newModuleFilesFixture(t, tc.git, tc.nested)
			var got []string
			err := walkModuleFiles(f.dir, func(path string, entry fs.DirEntry) error {
				if entry.Type()&os.ModeSymlink != 0 {
					t.Errorf("visitor received symlink %s", path)
				}
				if !entry.IsDir() {
					got = append(got, path)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			documentationAssertPaths(t, got, f.eligible)
			f.assertUnchanged(t)
		})
	}
}

func TestWalkModuleFilesPropagatesErrors(t *testing.T) {
	t.Run("visitor", func(t *testing.T) {
		f := newModuleFilesFixture(t, true, false)
		wantErr := errors.New("visitor stopped")
		calls := 0
		err := walkModuleFiles(f.dir, func(_ string, entry fs.DirEntry) error {
			if entry.IsDir() {
				return nil
			}
			calls++
			return wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("error = %v; want visitor error %v", err, wantErr)
		}
		if calls != 1 {
			t.Errorf("file visitor called %d times; want 1", calls)
		}
	})
	t.Run("missing root", func(t *testing.T) {
		err := walkModuleFiles(filepath.Join(t.TempDir(), "missing"), func(string, fs.DirEntry) error {
			t.Error("visitor called for missing root")
			return nil
		})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("error = %v; want fs.ErrNotExist", err)
		}
	})
}

func TestWalkModuleFilesPrunesIgnoredDirectoriesBeforeReading(t *testing.T) {
	f := newModuleFilesFixture(t, true, false)
	blocked := filepath.Join(f.dir, "artifacts")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o755); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("process can read mode-000 directories; cannot verify pre-descent pruning")
	} else if !errors.Is(err, fs.ErrPermission) {
		t.Fatal(err)
	}
	var got []string
	err := walkModuleFiles(f.dir, func(path string, entry fs.DirEntry) error {
		if !entry.IsDir() {
			got = append(got, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk descended into ignored unreadable directory: %v", err)
	}
	documentationAssertPaths(t, got, f.eligible)
}

func TestModuleFilesScannerParity(t *testing.T) {
	for _, git := range []bool{true, false} {
		name := "outside git"
		if git {
			name = "git"
		}
		t.Run(name, func(t *testing.T) {
			for _, scanner := range []string{"imports", "documentation"} {
				t.Run(scanner, func(t *testing.T) {
					f := newModuleFilesFixture(t, git, git)
					var dry, wet []string
					var err error
					want := f.imports
					if scanner == "imports" {
						dry, err = scanSelfImports(f.dir, moduleFilesOld, moduleFilesNew)
					} else {
						want = f.docs
						dry, err = updateDocumentationReferences(f.dir, moduleFilesOld, moduleFilesNew, true)
					}
					if err != nil {
						t.Fatalf("dry scan: %v", err)
					}
					documentationAssertPaths(t, dry, want)
					f.assertUnchanged(t)
					if scanner == "imports" {
						wet, err = updateSelfImports(f.dir, moduleFilesOld, moduleFilesNew)
					} else {
						wet, err = updateDocumentationReferences(f.dir, moduleFilesOld, moduleFilesNew, false)
					}
					if err != nil {
						t.Fatalf("wet scan parsed an excluded file or failed: %v", err)
					}
					documentationAssertPaths(t, wet, want)
					documentationAssertPaths(t, wet, dry)
					changed := make(map[string]bool)
					for _, path := range want {
						changed[path] = true
					}
					for path, before := range f.before {
						expected := before
						if changed[path] {
							if scanner == "imports" {
								expected = strings.ReplaceAll(before, "\""+moduleFilesOld+"/client\"", "\""+moduleFilesNew+"/client\"")
							} else if strings.HasSuffix(path, ".go") {
								expected = strings.ReplaceAll(before, "// Package widget uses "+moduleFilesOld+".", "// Package widget uses "+moduleFilesNew+".")
							} else {
								expected = strings.ReplaceAll(before, moduleFilesOld, moduleFilesNew)
							}
						}
						documentationAssertContent(t, path, expected)
					}
					for _, path := range f.links {
						info, err := os.Lstat(path)
						if err != nil || info.Mode()&os.ModeSymlink == 0 {
							t.Errorf("symlink %s changed: %v", path, err)
						}
					}
				})
			}
		})
	}
}
