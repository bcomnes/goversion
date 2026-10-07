package goversion

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// moduleScanPaths asks Git for tracked and nonignored untracked files, rather
// than enumerating ignored content. Ancestor entries let the walker prune whole
// directories before reading them while retaining tracked files and exceptions.
// A nil set means the directory is outside Git and filesystem discovery applies.
func moduleScanPaths(modDir string) (map[string]bool, error) {
	if _, err := gitRootDir(modDir); err != nil {
		return nil, nil
	}
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = modDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list module scan files: %w", err)
	}
	paths := map[string]bool{modDir: true}
	for _, name := range strings.Split(string(out), "\x00") {
		if name == "" {
			continue
		}
		path := filepath.Join(modDir, filepath.FromSlash(name))
		for !paths[path] {
			paths[path] = true
			parent := filepath.Dir(path)
			if parent == path {
				break
			}
			path = parent
		}
	}
	return paths, nil
}

// walkModuleFiles visits regular files owned by this module. Git-ineligible
// directories are pruned before descent; nested modules, vendor, Git internals,
// and symlinks are excluded regardless of tracking status.
func walkModuleFiles(modDir string, visit func(string, fs.DirEntry) error) error {
	return walkModuleTree(modDir, visit, nil)
}

// walkModuleTree reports module boundaries without descending into them.
// File and boundary callbacks are optional so discovery can collect identities
// before a rewriting pass visits any parent-module references.
func walkModuleTree(modDir string, visit func(string, fs.DirEntry) error, visitModule func(string) error) error {
	modDir = filepath.Clean(modDir)
	paths, err := moduleScanPaths(modDir)
	if err != nil {
		return err
	}
	return filepath.WalkDir(modDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != modDir {
				if entry.Name() == ".git" || entry.Name() == "vendor" || (paths != nil && !paths[path]) {
					return filepath.SkipDir
				}
				if fileExists(filepath.Join(path, "go.mod")) {
					if visitModule != nil {
						if err := visitModule(filepath.Join(path, "go.mod")); err != nil {
							return err
						}
					}
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !entry.Type().IsRegular() || (paths != nil && !paths[path]) {
			return nil
		}
		if visit != nil {
			return visit(path, entry)
		}
		return nil
	})
}

// nestedModulePaths collects declared child module families, not directory names.
// Their paths remain independent of the parent's major-version migration.
func nestedModulePaths(modDir string) ([]string, error) {
	var paths []string
	err := walkModuleTree(modDir, nil, func(filename string) error {
		info, err := os.Lstat(filename)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		declared := modfile.ModulePath(data)
		if declared == "" {
			return fmt.Errorf("missing module path in %s", filename)
		}
		base, _, ok := module.SplitPathVersion(declared)
		if !ok {
			return fmt.Errorf("invalid module path %q in %s", declared, filename)
		}
		paths = append(paths, base)
		return nil
	})
	return uniquePaths(paths), err
}

// matchesModulePath uses a path boundary so sibling modules with a shared text
// prefix (such as repo-tools) do not become self-references.
func matchesModulePath(path, modulePath string) bool {
	return path == modulePath || strings.HasPrefix(path, modulePath+"/")
}

// excludedModuleReference protects a nested module and all of its major versions.
func excludedModuleReference(path string, excludedModules []string) bool {
	for _, modulePath := range excludedModules {
		if matchesModulePath(path, modulePath) {
			return true
		}
	}
	return false
}
