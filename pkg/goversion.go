package goversion

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// VersionMeta describes a completed or simulated version bump.
type VersionMeta struct {
	// OldVersion is the version read before applying the bump.
	OldVersion string
	// NewVersion is the resulting version without a leading v prefix.
	NewVersion string
	// Tag is the exact local Git tag for NewVersion, including any nested-module prefix.
	Tag string
	// BumpType identifies the directive used to select NewVersion, such as patch, explicit, or from-git.
	BumpType string
	// UpdatedFiles lists files written by Run or files that DryRun predicts would change.
	UpdatedFiles []string
}

// VersionOptions configures a version bump without changing the caller's process working directory.
type VersionOptions struct {
	// WorkDir is the Go module working directory and defaults to the current directory.
	WorkDir string
	// VersionFile contains Version and defaults to ./version.go within WorkDir.
	VersionFile string
	// ExtraFiles are staged with the generated changes. Relative paths are resolved within WorkDir.
	ExtraFiles []string
	// BumpFiles have their first semantic version replaced. Relative paths are resolved within WorkDir.
	BumpFiles []string
	// PostBumpScript runs after updates and before commit. A relative path is resolved within WorkDir.
	PostBumpScript string
}

// normalizeVersion ensures the version string starts with a "v" if it's not "dev".
// If the version is "dev", we use "v0.0.0" as the base for bumping.
func normalizeVersion(v string) string {
	if v == "dev" {
		return "v0.0.0"
	}
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// parseSemVer extracts the numerical components and prerelease from a semver string.
// The expected input should be a canonical semver (with a leading "v").
func parseSemVer(version string) (major, minor, patch int, prerelease string, err error) {
	// Remove the "v" prefix.
	vWithoutPrefix := strings.TrimPrefix(version, "v")
	// Split off any prerelease part.
	parts := strings.SplitN(vWithoutPrefix, "-", 2)
	numParts := strings.Split(parts[0], ".")
	if len(numParts) != 3 {
		err = fmt.Errorf("unexpected version format: %s", version)
		return
	}

	if major, err = strconv.Atoi(numParts[0]); err != nil {
		return
	}
	if minor, err = strconv.Atoi(numParts[1]); err != nil {
		return
	}
	if patch, err = strconv.Atoi(numParts[2]); err != nil {
		return
	}
	if len(parts) == 2 {
		prerelease = parts[1]
	}
	return
}

// formatSemVer serializes bump calculations in the canonical form expected by x/mod/semver.
func formatSemVer(major, minor, patch int, prerelease string) string {
	base := fmt.Sprintf("v%d.%d.%d", major, minor, patch)
	if prerelease != "" {
		return base + "-" + prerelease
	}
	return base
}

// bumpVersion takes a valid, normalized semver string (with "v" prefix)
// and a bump directive to produce a new semver string.
// Supported bump types are: "major", "minor", "patch", "premajor", "preminor", "prepatch", "prerelease".
func bumpVersion(current, bump string) (string, error) {
	major, minor, patch, prerelease, err := parseSemVer(current)
	if err != nil {
		return "", err
	}

	switch bump {
	case "major":
		major++
		minor = 0
		patch = 0
		prerelease = ""
	case "minor":
		minor++
		patch = 0
		prerelease = ""
	case "patch":
		patch++
		prerelease = ""
	case "premajor":
		major++
		minor = 0
		patch = 0
		prerelease = "0"
	case "preminor":
		minor++
		patch = 0
		prerelease = "0"
	case "prepatch":
		patch++
		prerelease = "0"
	case "prerelease":
		if prerelease != "" {
			// Try to bump the last numeric part of the prerelease.
			parts := strings.Split(prerelease, ".")
			lastPart := parts[len(parts)-1]
			if n, err := strconv.Atoi(lastPart); err == nil {
				n++
				parts[len(parts)-1] = strconv.Itoa(n)
				prerelease = strings.Join(parts, ".")
			} else {
				// No numeric value detected at the end.
				prerelease = prerelease + ".0"
			}
		} else {
			// If there's no prerelease part, bump patch and start a prerelease.
			patch++
			prerelease = "0"
		}
	default:
		return "", fmt.Errorf("unknown bump argument: %s", bump)
	}

	return formatSemVer(major, minor, patch, prerelease), nil
}

// checkGit fails early when versioning cannot invoke the Git CLI.
func checkGit() error {
	cmd := exec.Command("git", "--version")
	if err := cmd.Run(); err != nil {
		return errors.New("git is not available on the system")
	}
	return nil
}

// determinePackageName returns the package name for the given file path.
// If the file exists, it extracts the package name using a regex.
// If the file does not exist, it scans the directory for any Go file (ignoring _test.go files)
// and returns the package name from the first file found. If none is found, it defaults to "version".
func determinePackageName(path string) (string, error) {
	// If the file exists, try to extract the package name.
	if _, err := os.Stat(path); err == nil {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read file %q: %v", path, err)
		}
		re := regexp.MustCompile(`(?m)^package\s+(\w+)`)
		if matches := re.FindSubmatch(data); len(matches) >= 2 {
			return string(matches[1]), nil
		}
		// Fall through if we can't parse the package name.
	}

	// If the file doesn't exist or its package name can't be determined,
	// scan the directory for Go files (ignoring test files) to get a package name.
	dir := filepath.Dir(path)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		// Only include regular Go files (ignore test files)
		return !strings.HasSuffix(fi.Name(), "_test.go") && strings.HasSuffix(fi.Name(), ".go")
	}, parser.PackageClauseOnly)
	if err != nil {
		return "", fmt.Errorf("failed to parse directory %q: %v", dir, err)
	}
	for pkgName := range pkgs {
		return pkgName, nil
	}

	// If no package could be determined, default to "version".
	return "version", nil
}

// writeVersionFile writes (or creates) the version file at the given path using the specified
// new version string (without the "v" prefix) and an appropriate package declaration.
func writeVersionFile(path, newVersion string) error {
	pkgName, err := determinePackageName(path)
	if err != nil {
		// If an error occurred during package determination, use a default.
		pkgName = "version"
	}
	content := fmt.Sprintf(`package %s

var (
	Version = "%s"
)
`, pkgName, newVersion)
	// Ensure the directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %q: %v", dir, err)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// updateGoMod rewrites the module path to match the major component of newVersion.
func updateGoMod(modDir, newVersion string) error {
	modPath := filepath.Join(modDir, "go.mod")
	data, err := os.ReadFile(modPath)
	if err != nil {
		return fmt.Errorf("reading go.mod: %w", err)
	}

	f, err := modfile.Parse(modPath, data, nil)
	if err != nil {
		return fmt.Errorf("parsing go.mod: %w", err)
	}
	if f.Module == nil {
		return fmt.Errorf("module directive not found")
	}

	basePath, _, _ := module.SplitPathVersion(f.Module.Mod.Path)
	maj := semver.Major("v" + newVersion)

	var newPath string
	if maj == "v0" || maj == "v1" {
		newPath = basePath
	} else {
		newPath = basePath + "/" + maj
	}

	// update both AST and logical path
	f.Module.Mod.Path = newPath
	if f.Module.Syntax != nil && len(f.Module.Syntax.Token) >= 2 {
		f.Module.Syntax.Token[1] = newPath
	}

	out, err := f.Format()
	if err != nil {
		return fmt.Errorf("formatting go.mod: %w", err)
	}
	if err := os.WriteFile(modPath, out, 0644); err != nil {
		return fmt.Errorf("writing go.mod: %w", err)
	}
	return nil
}

// readCurrentVersion reads the version file at the given path
// and extracts the version string. If the file does not exist,
// it first tries to get the latest tag from git in that directory,
// writes it into the version file, and returns it.
// If there are no tags or git fails, it falls back to “dev”.
func readCurrentVersion(path string) (string, error) {
	return readCurrentVersionForModule(path, filepath.Dir(path))
}

// readCurrentVersionForModule reads Version and uses only module-specific tags when initializing a missing file.
func readCurrentVersionForModule(path, moduleDir string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if fromGit, gitErr := getVersionFromGitDir(moduleDir); gitErr == nil {
				if err := writeVersionFile(path, fromGit); err != nil {
					return "", fmt.Errorf("failed to write version file from git tag: %w", err)
				}
				return fromGit, nil
			}
			// Fallback to dev
			defaultVersion := "dev"
			if err := writeVersionFile(path, defaultVersion); err != nil {
				return "", fmt.Errorf("failed to create default version file: %w", err)
			}
			return defaultVersion, nil
		}
		return "", fmt.Errorf("failed to read version file: %w", err)
	}

	// File exists: parse out the version string
	re := regexp.MustCompile(`Version\s*=\s*"([^"]+)"`)
	if matches := re.FindSubmatch(data); len(matches) >= 2 {
		return string(matches[1]), nil
	}
	return "", errors.New("failed to find version string in file")
}

// gitCommit stages the version file (plus any extra files provided),
// commits with a message equal to the new version (without the "v" prefix),
// and then tags the commit with the same version prefixed by "v".
func gitCommit(workDir, newVersion, tag string, extraFiles []string) error {
	// Ensure that the version file is included.
	files := extraFiles

	// Stage files.
	addArgs := append([]string{"add"}, files...)
	addCmd := exec.Command("git", addArgs...)
	addCmd.Dir = workDir
	var stderr bytes.Buffer
	addCmd.Stderr = &stderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %v, detail: %s", err, stderr.String())
	}

	// Commit changes.
	commitMsg := newVersion // commit message is the new version (without "v" prefix)
	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	commitCmd.Dir = workDir
	stderr.Reset()
	commitCmd.Stderr = &stderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %v, detail: %s", err, stderr.String())
	}

	// Tag the commit with the canonical module tag.
	tagCmd := exec.Command("git", "tag", tag)
	tagCmd.Dir = workDir
	stderr.Reset()
	tagCmd.Stderr = &stderr
	if err := tagCmd.Run(); err != nil {
		return fmt.Errorf("git tag failed: %v, detail: %s", err, stderr.String())
	}

	return nil
}

// getVersionFromGitDir retrieves the most recent tag from git in the given directory
// and strips off any leading "v".
func getVersionFromGitDir(dir string) (string, error) {
	gitRoot, err := gitRootDir(dir)
	if err != nil {
		return "", err
	}
	prefix, err := moduleTagPrefix(gitRoot, dir)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0", "--match", prefix+"v[0-9]*")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get version from git in %q: %v", dir, err)
	}
	tag := strings.TrimSpace(string(out))
	version := strings.TrimPrefix(tag, prefix)
	if !semver.IsValid(version) {
		return "", fmt.Errorf("module tag %q is not a valid semantic version", tag)
	}
	return strings.TrimPrefix(version, "v"), nil
}

// gitRootDir returns the canonical top-level directory for the Git repository containing dir.
func gitRootDir(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("find git repository root from %q: %w", dir, err)
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

// moduleTagPrefix returns the slash-separated tag prefix for moduleDir within gitRoot.
func moduleTagPrefix(gitRoot, moduleDir string) (string, error) {
	relative, err := filepath.Rel(gitRoot, moduleDir)
	if err != nil {
		return "", fmt.Errorf("resolve module directory relative to git root: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("Go module directory %q is outside Git root %q", moduleDir, gitRoot)
	}
	if relative == "." {
		return "", nil
	}
	return filepath.ToSlash(relative) + "/", nil
}

// ModuleVersionTag returns the canonical Git tag for a module directory and semantic version.
func ModuleVersionTag(gitRoot, moduleDir, version string) (string, error) {
	prefix, err := moduleTagPrefix(gitRoot, moduleDir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return prefix + version, nil
}

// Run applies a semantic version bump and creates its local Git commit and tag.
//
// versionFilePath identifies the Go source file containing Version.
// versionArg accepts major, minor, patch, premajor, preminor, prepatch, prerelease, from-git, dev, or an explicit semantic version.
// extraFiles are staged with the generated version changes.
// bumpFiles have their first semantic version replaced with the new version.
// postBumpScript runs after file updates and before the commit with GOVERSION_OLD_VERSION and GOVERSION_NEW_VERSION in its environment.
//
// For a major bump to v2 or newer, Run updates the module path in go.mod and rewrites self-imports.
// Run requires unrelated worktree changes to be absent and returns metadata for the files it updates.
func Run(versionFilePath, versionArg string, extraFiles []string, bumpFiles []string, postBumpScript string) (VersionMeta, error) {
	return runWithOptions(VersionOptions{VersionFile: versionFilePath, ExtraFiles: extraFiles, BumpFiles: bumpFiles, PostBumpScript: postBumpScript}, versionArg, false)
}

// RunWithOptions applies a semantic version bump using repository-confined paths relative to options.WorkDir.
func RunWithOptions(options VersionOptions, versionArg string) (VersionMeta, error) {
	return runWithOptions(options, versionArg, true)
}

// runWithOptions implements versioning with optional repository path confinement.
func runWithOptions(options VersionOptions, versionArg string, confinePaths bool) (VersionMeta, error) {
	var meta VersionMeta
	workDir, gitRoot, versionFilePath, extraFiles, bumpFiles, postBumpScript, modDir, err := resolveVersionOptions(options, confinePaths)
	if err != nil {
		return meta, err
	}

	// 1. Ensure git is available
	if err := checkGit(); err != nil {
		return meta, err
	}

	// 2. Read the current version
	currentVersionRaw, err := readCurrentVersionForModule(versionFilePath, modDir)
	if err != nil {
		return meta, err
	}
	meta.OldVersion = currentVersionRaw

	// Normalize
	normalizedCurrent := normalizeVersion(currentVersionRaw)

	// 3. Determine new version
	switch versionArg {
	case "major", "minor", "patch", "premajor", "preminor", "prepatch", "prerelease":
		bumped, err := bumpVersion(normalizedCurrent, versionArg)
		if err != nil {
			return meta, err
		}
		meta.NewVersion = strings.TrimPrefix(bumped, "v")
		meta.BumpType = versionArg
	case "from-git":
		fromGit, err := getVersionFromGitDir(modDir)
		if err != nil {
			return meta, err
		}
		meta.NewVersion = fromGit
		meta.BumpType = "from-git"
	default:
		explicit := versionArg
		if explicit != "dev" && !strings.HasPrefix(explicit, "v") {
			explicit = "v" + explicit
		}
		if explicit != "dev" && !semver.IsValid(explicit) {
			return meta, fmt.Errorf("explicit version %q is not valid semver", explicit)
		}
		meta.NewVersion = strings.TrimPrefix(explicit, "v")
		meta.BumpType = "explicit"
	}

	// Prevent no-op
	if meta.NewVersion == meta.OldVersion {
		return meta, fmt.Errorf("new version (%s) is the same as the current version", meta.NewVersion)
	}

	// Prepare allowed list for dirty check
	allowed := make([]string, len(extraFiles))
	copy(allowed, extraFiles)
	allowed = append(allowed, versionFilePath)

	meta.Tag, err = ModuleVersionTag(gitRoot, modDir, meta.NewVersion)
	if err != nil {
		return meta, err
	}

	// Detect module changes for major bumps.
	var oldModPath string
	hasGoMod := fileExists(filepath.Join(modDir, "go.mod"))
	if meta.BumpType == "major" && hasGoMod {
		data, err := os.ReadFile(filepath.Join(modDir, "go.mod"))
		if err != nil {
			return meta, fmt.Errorf("reading go.mod: %w", err)
		}
		f, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			return meta, fmt.Errorf("parsing go.mod: %w", err)
		}
		oldModPath = f.Module.Mod.Path
		allowed = append(allowed, filepath.Join(modDir, "go.mod"))
	}

	// 5. Check for uncommitted files
	if err := checkUncommittedFiles(workDir, allowed); err != nil {
		return meta, err
	}

	// 6. Write version file
	if err := writeVersionFile(versionFilePath, meta.NewVersion); err != nil {
		return meta, err
	}

	// 6.5. Update go.mod if needed
	var newModPath string
	if meta.BumpType == "major" && hasGoMod {
		if err := updateGoMod(modDir, meta.NewVersion); err != nil {
			return meta, err
		}
		// Re-read new module path
		data, err := os.ReadFile(filepath.Join(modDir, "go.mod"))
		if err != nil {
			return meta, fmt.Errorf("reading go.mod: %w", err)
		}
		f, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			return meta, fmt.Errorf("parsing go.mod: %w", err)
		}
		newModPath = f.Module.Mod.Path
	}

	// 6.6. Rewrite self-imports
	var rewritten []string
	if newModPath != "" {
		rewritten, err = updateSelfImports(modDir, oldModPath, newModPath)
		if err != nil {
			return meta, err
		}
	}

	// 6.7. Process bump files
	var bumpedFiles []string
	for _, bf := range bumpFiles {
		if err := findAndReplaceSemver(bf, meta.NewVersion); err != nil {
			// Log warning but don't fail
			fmt.Fprintf(os.Stderr, "Warning: failed to bump version in %s: %v\n", bf, err)
		} else {
			bumpedFiles = append(bumpedFiles, bf)
		}
	}

	// 6.8. Run post-bump script if provided
	if postBumpScript != "" {
		if err := runPostBumpScript(workDir, postBumpScript, meta.OldVersion, meta.NewVersion); err != nil {
			return meta, fmt.Errorf("post-bump script failed: %w", err)
		}
	}

	// 7. Stage, commit, and tag
	filesToCommit := make([]string, len(extraFiles))
	copy(filesToCommit, extraFiles)
	filesToCommit = append(filesToCommit, versionFilePath)
	if meta.BumpType == "major" && hasGoMod {
		filesToCommit = append(filesToCommit, filepath.Join(modDir, "go.mod"))
	}
	filesToCommit = append(filesToCommit, rewritten...)
	filesToCommit = append(filesToCommit, bumpedFiles...)
	if err := gitCommit(workDir, meta.NewVersion, meta.Tag, filesToCommit); err != nil {
		return meta, err
	}

	meta.UpdatedFiles = append([]string{versionFilePath}, rewritten...)
	meta.UpdatedFiles = append(meta.UpdatedFiles, bumpedFiles...)
	if meta.BumpType == "major" && hasGoMod {
		meta.UpdatedFiles = append([]string{filepath.Join(modDir, "go.mod")}, meta.UpdatedFiles...)
	}

	return meta, nil
}

// DryRun calculates a version bump without changing files or Git state.
//
// It reports the resulting version and every file that Run would update, including versionFilePath, major-version module changes, rewritten self-imports, and bumpFiles.
func DryRun(versionFilePath, versionArg string, bumpFiles []string) (VersionMeta, error) {
	return dryRunWithOptions(VersionOptions{VersionFile: versionFilePath, BumpFiles: bumpFiles}, versionArg, false)
}

// DryRunWithOptions calculates a bump using repository-confined paths relative to options.WorkDir without changing Git state.
func DryRunWithOptions(options VersionOptions, versionArg string) (VersionMeta, error) {
	return dryRunWithOptions(options, versionArg, true)
}

// dryRunWithOptions implements dry-run versioning with optional repository path confinement.
func dryRunWithOptions(options VersionOptions, versionArg string, confinePaths bool) (VersionMeta, error) {
	var meta VersionMeta
	_, gitRoot, versionFilePath, _, bumpFiles, _, modDir, err := resolveVersionOptions(options, confinePaths)
	if err != nil {
		return meta, err
	}

	// 1. Read current version
	cur, err := readCurrentVersionForModule(versionFilePath, modDir)
	if err != nil {
		return meta, err
	}
	meta.OldVersion = cur

	// 2. Compute NewVersion and BumpType (same logic as Run)
	normalized := normalizeVersion(cur)
	switch versionArg {
	case "major", "minor", "patch", "premajor", "preminor", "prepatch", "prerelease":
		bumped, err := bumpVersion(normalized, versionArg)
		if err != nil {
			return meta, err
		}
		meta.NewVersion = strings.TrimPrefix(bumped, "v")
		meta.BumpType = versionArg
	case "from-git":
		fromGit, err := getVersionFromGitDir(modDir)
		if err != nil {
			return meta, err
		}
		meta.NewVersion = fromGit
		meta.BumpType = "from-git"
	default:
		expl := versionArg
		if expl != "dev" && !strings.HasPrefix(expl, "v") {
			expl = "v" + expl
		}
		if expl != "dev" && !semver.IsValid(expl) {
			return meta, fmt.Errorf("explicit version %q is not valid semver", expl)
		}
		meta.NewVersion = strings.TrimPrefix(expl, "v")
		meta.BumpType = "explicit"
	}

	// 3. Prevent no-op
	if meta.NewVersion == meta.OldVersion {
		return meta, fmt.Errorf("new version (%s) is the same as the current version", meta.NewVersion)
	}

	meta.Tag, err = ModuleVersionTag(gitRoot, modDir, meta.NewVersion)
	if err != nil {
		return meta, err
	}

	// 4. Always include version.go
	files := []string{versionFilePath}

	// 5. For major bumps, also include go.mod and scan imports
	if meta.BumpType == "major" && fileExists(filepath.Join(modDir, "go.mod")) {
		gomodPath := filepath.Join(modDir, "go.mod")
		files = append(files, gomodPath)

		// Parse old module path
		data, _ := os.ReadFile(gomodPath)
		f, _ := modfile.Parse("go.mod", data, nil)
		oldMod := f.Module.Mod.Path

		// Compute new module path
		base, _, _ := module.SplitPathVersion(oldMod)
		maj := semver.Major("v" + meta.NewVersion)
		var newMod string
		if maj == "v0" || maj == "v1" {
			newMod = base
		} else {
			newMod = base + "/" + maj
		}

		// Scan for all .go files needing import updates
		if more, err := scanSelfImports(modDir, oldMod, newMod); err == nil {
			files = append(files, more...)
		}
	}

	// 6. Check bump files
	for _, bf := range bumpFiles {
		if _, err := os.Stat(bf); err == nil {
			files = append(files, bf)
		}
	}

	meta.UpdatedFiles = files
	return meta, nil
}

// resolveVersionOptions resolves workdir-relative paths and rejects repository escapes.
func resolveVersionOptions(options VersionOptions, confinePaths bool) (workDir, gitRoot, versionFile string, extraFiles, bumpFiles []string, postBumpScript, modDir string, err error) {
	workDir = options.WorkDir
	if workDir == "" {
		workDir = "."
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return "", "", "", nil, nil, "", "", fmt.Errorf("resolve work directory: %w", err)
	}
	info, err := os.Stat(workDir)
	if err != nil {
		return "", "", "", nil, nil, "", "", fmt.Errorf("stat work directory %q: %w", workDir, err)
	}
	if !info.IsDir() {
		return "", "", "", nil, nil, "", "", fmt.Errorf("work directory %q is not a directory", workDir)
	}
	resolve := func(path string) string {
		if path == "" || filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(workDir, path)
	}
	versionFile = options.VersionFile
	if versionFile == "" {
		versionFile = "./version.go"
	}
	versionFile = resolve(versionFile)
	for _, path := range options.ExtraFiles {
		extraFiles = append(extraFiles, resolve(path))
	}
	for _, path := range options.BumpFiles {
		bumpFiles = append(bumpFiles, resolve(path))
	}
	postBumpScript = resolve(options.PostBumpScript)

	canonicalVersionFile, err := canonicalPath(versionFile)
	if err != nil {
		return "", "", "", nil, nil, "", "", fmt.Errorf("resolve version file %q: %w", versionFile, err)
	}
	modDir, err = locateGoModDir(filepath.Dir(canonicalVersionFile))
	hasGoMod := err == nil

	if confinePaths {
		gitRoot, err = gitRootDir(workDir)
		if err != nil {
			return "", "", "", nil, nil, "", "", err
		}
		paths := append([]string{versionFile}, extraFiles...)
		paths = append(paths, bumpFiles...)
		if postBumpScript != "" {
			paths = append(paths, postBumpScript)
		}
		for _, path := range paths {
			if err := validateRepositoryPath(gitRoot, path); err != nil {
				return "", "", "", nil, nil, "", "", err
			}
		}
	} else {
		gitRoot, err = gitRootDir(filepath.Dir(canonicalVersionFile))
		if err != nil {
			gitRoot = filepath.Dir(canonicalVersionFile)
		}
	}
	if !hasGoMod {
		modDir = gitRoot
	}
	return workDir, gitRoot, versionFile, extraFiles, bumpFiles, postBumpScript, modDir, nil
}

// validateRepositoryPath rejects lexical and existing-symlink escapes from gitRoot.
func validateRepositoryPath(gitRoot, path string) error {
	canonical, err := canonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}
	inside, err := pathWithin(gitRoot, canonical)
	if err != nil {
		return err
	}
	if !inside {
		return fmt.Errorf("path %q is outside Git repository %q", path, gitRoot)
	}
	return nil
}

// canonicalPath resolves symlinks in the deepest existing ancestor of path.
func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

// pathWithin reports whether path is at or below root.
func pathWithin(root, path string) (bool, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false, fmt.Errorf("resolve path relative to Git root: %w", err)
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative), nil
}

// fileExists reports whether path names an existing file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// findAndReplaceSemver finds the first semantic version in a file and replaces it with newVersion.
// It uses the official semver regex and does NOT support 'v' prefixes.
func findAndReplaceSemver(filepath, newVersion string) error {
	// Read file
	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Official semver regex with named capture groups from semver.org
	// Removed anchors (^ and $) to find versions anywhere in the file
	semverPattern := `(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)(?:-(?P<prerelease>(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?`

	re, err := regexp.Compile(semverPattern)
	if err != nil {
		return fmt.Errorf("failed to compile regex: %w", err)
	}

	// Find all matches with their positions
	allMatches := re.FindAllIndex(content, -1)
	if len(allMatches) == 0 {
		return fmt.Errorf("no semantic version found in file")
	}

	// Check each match to find the first one not preceded by 'v' or 'V'
	var validMatch []int
	for _, match := range allMatches {
		start := match[0]
		// Check if there's a character before this match
		if start > 0 {
			prevChar := content[start-1]
			if prevChar == 'v' || prevChar == 'V' {
				// Skip this match as it's part of a v-prefixed version
				continue
			}
		}
		// This is a valid match
		validMatch = match
		break
	}

	if validMatch == nil {
		return fmt.Errorf("no semantic version found in file")
	}

	// Get the matched version string
	matchedVersion := content[validMatch[0]:validMatch[1]]

	// Replace only the first valid occurrence
	newContent := bytes.Replace(content, matchedVersion, []byte(newVersion), 1)

	// Write back
	if err := os.WriteFile(filepath, newContent, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// locateGoModDir walks up from startDir until it finds go.mod.
// Returns the directory containing go.mod, or ErrNotExist if none found.
func locateGoModDir(startDir string) (string, error) {
	d := startDir
	for {
		candidate := filepath.Join(d, "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", os.ErrNotExist
}

// checkUncommittedFiles prevents a release commit from including unrelated worktree changes.
func checkUncommittedFiles(workDir string, allowed []string) error {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}

	gitRoot, err := gitRootDir(workDir)
	if err != nil {
		return err
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, f := range allowed {
		abs, err := filepath.Abs(f)
		if err != nil {
			return fmt.Errorf("failed to resolve path %q: %w", f, err)
		}
		allowedSet[abs] = struct{}{}
	}

	var disallowed []string
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) < 4 {
			continue
		}
		path := string(bytes.TrimSpace(line[3:]))
		absPath, err := filepath.Abs(filepath.Join(gitRoot, path))
		if err != nil {
			continue
		}
		if _, ok := allowedSet[absPath]; !ok {
			disallowed = append(disallowed, path)
		}
	}

	if len(disallowed) > 0 {
		return fmt.Errorf("working directory is dirty; uncommitted files not included in commit: %v", disallowed)
	}
	return nil
}

// scanSelfImports returns the list of .go files under modDir
// whose imports would be rewritten from oldMod → newMod.
func scanSelfImports(modDir, oldMod, newMod string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(modDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			// skip unparsable files
			return nil
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(p, oldMod) {
				matches = append(matches, path)
				break
			}
		}
		return nil
	})
	return matches, err
}

// updateSelfImports walks all .go files under modDir, updating imports from oldMod to newMod.
// Returns the list of files modified.
func updateSelfImports(modDir, oldMod, newMod string) ([]string, error) {
	var modified []string
	err := filepath.WalkDir(modDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip vendor directories
		if d.IsDir() {
			if d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		// Only consider .go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		fset := token.NewFileSet()
		fileAst, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}

		changed := false
		for _, imp := range fileAst.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			if strings.HasPrefix(p, oldMod) {
				newPath := strings.Replace(p, oldMod, newMod, 1)
				imp.Path.Value = strconv.Quote(newPath)
				changed = true
			}
		}

		if !changed {
			return nil
		}

		// Overwrite file with updated AST
		outFile, err := os.Create(path)
		if err != nil {
			return err
		}
		defer outFile.Close()
		if err := printer.Fprint(outFile, fset, fileAst); err != nil {
			return err
		}
		modified = append(modified, path)
		return nil
	})

	return modified, err
}

// runPostBumpScript executes the post-bump script with version information in environment variables.
func runPostBumpScript(workDir, scriptPath, oldVersion, newVersion string) error {
	// Check if script exists and is executable
	info, err := os.Stat(scriptPath)
	if err != nil {
		return fmt.Errorf("script not found: %w", err)
	}

	// On Unix systems, check if executable
	if info.Mode()&0111 == 0 && runtime.GOOS != "windows" {
		return fmt.Errorf("script is not executable: %s", scriptPath)
	}

	// Prepare the command
	cmd := exec.Command(scriptPath)
	cmd.Dir = workDir

	// Set environment variables
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GOVERSION_OLD_VERSION=%s", oldVersion),
		fmt.Sprintf("GOVERSION_NEW_VERSION=%s", newVersion),
	)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the script
	err = cmd.Run()

	// Always print output if there is any
	if stdout.Len() > 0 {
		fmt.Print(stdout.String())
	}
	if stderr.Len() > 0 {
		fmt.Fprint(os.Stderr, stderr.String())
	}

	if err != nil {
		return fmt.Errorf("script execution failed: %w", err)
	}

	return nil
}
