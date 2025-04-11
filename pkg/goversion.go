package goversion

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

// VersionMeta holds metadata about the version bump operation.
type VersionMeta struct {
	OldVersion string // The version before bumping.
	NewVersion string // The new version after bumping.
	BumpType   string // How the version was bumped (e.g. "major", "explicit", "from-git", etc.).
	// Add additional fields as needed.
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

// formatSemVer constructs a canonical semver string (with the "v" prefix)
// from its components.
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

// checkGit verifies that git is available on the system.
func checkGit() error {
	cmd := exec.Command("git", "--version")
	if err := cmd.Run(); err != nil {
		return errors.New("git is not available on the system")
	}
	return nil
}

// checkUncommittedFiles ensures that only the allowed files are dirty in the working directory.
func checkUncommittedFiles(allowed []string) error {
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
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
	lines := bytes.Split(out, []byte("\n"))
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		path := string(bytes.TrimSpace(line[3:]))
		absPath, err := filepath.Abs(path)
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
		if matches := re.FindSubmatch(data); matches != nil && len(matches) >= 2 {
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

// readCurrentVersion reads the version file at the given path
// and extracts the version string from a line that looks like:
//   Version = "dev" or Version = "1.2.3".
// If the file does not exist, it creates the file with a default version "dev".
func readCurrentVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File does not exist: create it with default version "dev".
			defaultVersion := "dev"
			if err := writeVersionFile(path, defaultVersion); err != nil {
				return "", fmt.Errorf("failed to create default version file: %v", err)
			}
			return defaultVersion, nil
		}
		return "", fmt.Errorf("failed to read version file: %v", err)
	}
	// Look for a line like: Version = "..."
	re := regexp.MustCompile(`Version\s*=\s*"([^"]+)"`)
	matches := re.FindSubmatch(data)
	if matches == nil || len(matches) < 2 {
		return "", errors.New("failed to find version string in file")
	}
	return string(matches[1]), nil
}

// gitCommit stages the version file (plus any extra files provided),
// commits with a message equal to the new version (without the "v" prefix),
// and then tags the commit with the same version prefixed by "v".
func gitCommit(newVersion string, extraFiles []string) error {
	// Ensure that the version file is included.
	files := extraFiles

	// Stage files.
	addArgs := append([]string{"add"}, files...)
	addCmd := exec.Command("git", addArgs...)
	var stderr bytes.Buffer
	addCmd.Stderr = &stderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %v, detail: %s", err, stderr.String())
	}

	// Commit changes.
	commitMsg := newVersion // commit message is the new version (without "v" prefix)
	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	stderr.Reset()
	commitCmd.Stderr = &stderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %v, detail: %s", err, stderr.String())
	}

	// Tag the commit with "v" prefix.
	tagName := "v" + newVersion
	tagCmd := exec.Command("git", "tag", tagName)
	stderr.Reset()
	tagCmd.Stderr = &stderr
	if err := tagCmd.Run(); err != nil {
		return fmt.Errorf("git tag failed: %v, detail: %s", err, stderr.String())
	}

	return nil
}

// getVersionFromGit retrieves the most recent tag from git and removes the "v" prefix.
func getVersionFromGit() (string, error) {
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get version from git: %v", err)
	}
	tag := strings.TrimSpace(string(out))
	return strings.TrimPrefix(tag, "v"), nil
}

// Run is the main function for the goversion library.
// It accepts a path to the Go file containing a version declaration,
// a version argument (which can be one of the bump keywords or an explicit version),
// and a slice of extra files to include in the commit.
// Supported versionArg values are:
//   [<newversion> | major | minor | patch | premajor | preminor | prepatch | prerelease | from-git]
// It now returns metadata about the operation.
func Run(versionFilePath string, versionArg string, extraFiles []string) (VersionMeta, error) {
	var meta VersionMeta

	// Step 1: Ensure git is installed.
	if err := checkGit(); err != nil {
		return meta, err
	}

	// Step 2: Read the current version from the specified file.
	currentVersionRaw, err := readCurrentVersion(versionFilePath)
	if err != nil {
		return meta, err
	}
	meta.OldVersion = currentVersionRaw

	// Normalize the version for semver operations.
	normalizedCurrent := normalizeVersion(currentVersionRaw)

	var newVersion string

	// Step 3: Determine the new version.
	switch versionArg {
	case "major", "minor", "patch", "premajor", "preminor", "prepatch", "prerelease":
		bumped, err := bumpVersion(normalizedCurrent, versionArg)
		if err != nil {
			return meta, err
		}
		newVersion = strings.TrimPrefix(bumped, "v")
		meta.BumpType = versionArg
	case "from-git":
		fromGit, err := getVersionFromGit()
		if err != nil {
			return meta, err
		}
		newVersion = fromGit
		meta.BumpType = "from-git"
	default:
		// Treat the argument as an explicit version.
		explicit := versionArg
		if explicit != "dev" && !strings.HasPrefix(explicit, "v") {
			explicit = "v" + explicit
		}
		if explicit != "dev" && !semver.IsValid(explicit) {
			return meta, fmt.Errorf("explicit version %q is not valid semver", explicit)
		}
		newVersion = strings.TrimPrefix(explicit, "v")
		meta.BumpType = "explicit"
	}
	meta.NewVersion = newVersion

	// Step 4: Prevent a no-op version bump.
	if newVersion == currentVersionRaw {
		return meta, fmt.Errorf("new version (%s) is the same as the current version", newVersion)
	}

	// Step 5: Check for dirty files outside of extraFiles.
	if err := checkUncommittedFiles(extraFiles); err != nil {
		return meta, err
	}

	// Step 6: Write the new version to the file.
	if err := writeVersionFile(versionFilePath, newVersion); err != nil {
		return meta, err
	}

	// Step 7: Stage, commit, and tag.
	if err := gitCommit(newVersion, extraFiles); err != nil {
		return meta, err
	}

	return meta, nil
}

// DryRun is a new function that simulates the version bump operation without
// writing any changes to disk or modifying the git repository. It returns the
// VersionMeta data that would be generated by a real bump.
func DryRun(versionFilePath string, versionArg string) (VersionMeta, error) {
	var meta VersionMeta

	// Read the current version.
	currentVersionRaw, err := readCurrentVersion(versionFilePath)
	if err != nil {
		return meta, err
	}
	meta.OldVersion = currentVersionRaw

	// Normalize the version for semver operations.
	normalizedCurrent := normalizeVersion(currentVersionRaw)

	var newVersion string

	// Determine the new version based on the versionArg.
	switch versionArg {
	case "major", "minor", "patch", "premajor", "preminor", "prepatch", "prerelease":
		bumped, err := bumpVersion(normalizedCurrent, versionArg)
		if err != nil {
			return meta, err
		}
		newVersion = strings.TrimPrefix(bumped, "v")
		meta.BumpType = versionArg
	case "from-git":
		fromGit, err := getVersionFromGit()
		if err != nil {
			return meta, err
		}
		newVersion = fromGit
		meta.BumpType = "from-git"
	default:
		explicit := versionArg
		if explicit != "dev" && !strings.HasPrefix(explicit, "v") {
			explicit = "v" + explicit
		}
		if explicit != "dev" && !semver.IsValid(explicit) {
			return meta, fmt.Errorf("explicit version %q is not valid semver", explicit)
		}
		newVersion = strings.TrimPrefix(explicit, "v")
		meta.BumpType = "explicit"
	}
	meta.NewVersion = newVersion

	// Prevent a no-op version bump.
	if newVersion == currentVersionRaw {
		return meta, fmt.Errorf("new version (%s) is the same as the current version", newVersion)
	}

	// DryRun: Do not check dirty files, write changes, or commit.
	// Simply return the metadata that would be used.
	return meta, nil
}
