package goversion

import (
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
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
			if path != modDir && (entry.Name() == ".git" || entry.Name() == "vendor" || (paths != nil && !paths[path]) || fileExists(filepath.Join(path, "go.mod"))) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || (paths != nil && !paths[path]) {
			return nil
		}
		return visit(path, entry)
	})
}
