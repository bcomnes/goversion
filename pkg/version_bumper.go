package goversion

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// VersionPattern represents a pattern for finding versions in files
type VersionPattern struct {
	Pattern *regexp.Regexp
	Name    string
}

// CommonVersionPatterns contains common patterns for finding version strings in files
var CommonVersionPatterns = []VersionPattern{
	{
		Pattern: regexp.MustCompile(`("version"\s*:\s*")v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)(")`),
		Name:    "JSON version field",
	},
	{
		Pattern: regexp.MustCompile(`(?i)(VERSION\s*[:=]\s*["']?)v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)(["']?)`),
		Name:    "VERSION assignment",
	},
	{
		Pattern: regexp.MustCompile(`(@version\s+)v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)`),
		Name:    "doc comment version",
	},
	{
		Pattern: regexp.MustCompile(`(<version>)v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)(</version>)`),
		Name:    "XML version tag",
	},
	{
		Pattern: regexp.MustCompile(`(?i)(version\s*[:=]\s*["']?)v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)(["']?)`),
		Name:    "version assignment",
	},
	{
		Pattern: regexp.MustCompile(`(?i)(#\s*version\s+)v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)`),
		Name:    "markdown version header",
	},
	{
		Pattern: regexp.MustCompile(`(?i)(current\s+version.*?)v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)`),
		Name:    "current version text",
	},
	{
		Pattern: regexp.MustCompile(`(@)v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)(\s|$)`),
		Name:    "at version",
	},
	{
		Pattern: regexp.MustCompile(`(?i)(install\s+version\s+)v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)`),
		Name:    "install version text",
	},
	{
		Pattern: regexp.MustCompile(`(version\s*=\s*")v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)(")`),
		Name:    "TOML version field",
	},
}

// MainVersionPatterns contains patterns that are more likely to be the main version field.
// These patterns are designed to match version declarations that are typically
// the primary version of a project, not dependency versions or other references.
var MainVersionPatterns = []VersionPattern{
	{
		Pattern: regexp.MustCompile(`^\s*"version"\s*:\s*"v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)"`),
		Name:    "root JSON version field",
	},
	{
		Pattern: regexp.MustCompile(`^\s*version\s*=\s*"v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)"`),
		Name:    "root TOML version field",
	},
	{
		Pattern: regexp.MustCompile(`(?i)^\s*VERSION\s*[:=]\s*["']?v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)["']?`),
		Name:    "root VERSION assignment",
	},
}

// VersionMatch represents a found version in a file
type VersionMatch struct {
	Line        int
	StartIndex  int
	EndIndex    int
	FullMatch   string
	Version     string
	Pattern     VersionPattern
	Prefix      string
	Suffix      string
}

// FindVersionsInFile searches for version patterns in the given file
func FindVersionsInFile(filePath string) ([]VersionMatch, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", filePath, err)
	}

	var matches []VersionMatch
	lines := strings.Split(string(data), "\n")

	// Track matched positions to avoid duplicates
	type posKey struct {
		line  int
		start int
		end   int
	}
	seen := make(map[posKey]bool)

	for lineNum, line := range lines {
		for _, vp := range CommonVersionPatterns {
			allMatches := vp.Pattern.FindAllStringSubmatchIndex(line, -1)
			for _, match := range allMatches {
				if len(match) >= 6 { // Need at least 3 groups (full match, prefix, version, suffix)
					key := posKey{line: lineNum, start: match[0], end: match[1]}
					if seen[key] {
						continue // Skip duplicate match at same position
					}
					seen[key] = true

					vm := VersionMatch{
						Line:       lineNum + 1,
						StartIndex: match[0],
						EndIndex:   match[1],
						FullMatch:  line[match[0]:match[1]],
						Pattern:    vp,
					}

					// Extract the version number (usually in the second capture group)
					if len(match) >= 4 {
						vm.Prefix = line[match[2]:match[3]]
					}
					if len(match) >= 6 {
						vm.Version = line[match[4]:match[5]]
					}
					if len(match) >= 8 {
						vm.Suffix = line[match[6]:match[7]]
					}

					// Normalize the version (remove 'v' prefix if present)
					vm.Version = strings.TrimPrefix(vm.Version, "v")

					matches = append(matches, vm)
				}
			}
		}
	}

	return matches, nil
}

// FindMainVersionInFile searches for the primary version field in a file.
// It uses intelligent detection to find the most likely "main" version:
// - For JSON: Prefers top-level "version" fields over nested ones
// - For TOML: Prefers version fields in [package] sections
// - For other formats: Uses pattern matching to find the most likely main version
// This helps avoid updating dependency versions or other secondary version references.
func FindMainVersionInFile(filePath string) (*VersionMatch, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", filePath, err)
	}

	lines := strings.Split(string(data), "\n")

	// First, try to find a main version pattern
	for lineNum, line := range lines {
		for _, vp := range MainVersionPatterns {
			if matches := vp.Pattern.FindStringSubmatchIndex(line); matches != nil && len(matches) >= 4 {
				// Extract just the version number from the first capture group
				version := line[matches[2]:matches[3]]
				version = strings.TrimPrefix(version, "v")

				vm := VersionMatch{
					Line:       lineNum + 1,
					StartIndex: matches[0],
					EndIndex:   matches[1],
					FullMatch:  line[matches[0]:matches[1]],
					Version:    version,
					Pattern:    vp,
				}

				// For root patterns, we need to reconstruct prefix and suffix
				if strings.Contains(line[matches[0]:matches[1]], "\"") {
					parts := strings.Split(line[matches[0]:matches[1]], version)
					if len(parts) >= 2 {
						vm.Prefix = parts[0]
						vm.Suffix = parts[1]
					}
				}

				return &vm, nil
			}
		}
	}

	// If no main version found, fall back to finding the first version
	matches, err := FindVersionsInFile(filePath)
	if err != nil {
		return nil, err
	}

	if len(matches) > 0 {
		// For JSON files, prefer a top-level version field
		if strings.HasSuffix(filePath, ".json") {
			for i, match := range matches {
				// Check if this looks like a top-level field by checking indentation
				if match.Line <= len(lines) {
					line := lines[match.Line-1]
					if strings.HasPrefix(strings.TrimLeft(line, " \t"), "\"version\"") {
						// Count leading spaces/tabs
						leadingSpace := len(line) - len(strings.TrimLeft(line, " \t"))
						if leadingSpace <= 2 { // Likely top-level
							return &matches[i], nil
						}
					}
				}
			}
		}

		// For TOML files, prefer [package] section version
		if strings.HasSuffix(filePath, ".toml") {
			inPackageSection := false
			for i, match := range matches {
				if match.Line <= len(lines) {
					// Check previous lines for [package] section
					for j := match.Line - 1; j > 0 && j > match.Line - 10; j-- {
						if strings.Contains(lines[j-1], "[package]") {
							inPackageSection = true
							break
						}
						if strings.HasPrefix(strings.TrimSpace(lines[j-1]), "[") &&
						   strings.HasSuffix(strings.TrimSpace(lines[j-1]), "]") {
							// Another section started
							break
						}
					}
					if inPackageSection {
						return &matches[i], nil
					}
				}
			}
		}

		// Default to first match
		return &matches[0], nil
	}

	return nil, nil
}

// ReplaceVersionInFile replaces all found versions in a file with the new version
func ReplaceVersionInFile(filePath string, newVersion string, matches []VersionMatch) error {
	// Read the file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading file %s: %w", filePath, err)
	}

	// Convert to string for replacement
	content := string(data)

	// Group matches by line and sort by position within line (reverse order)
	lineMatches := make(map[int][]VersionMatch)
	for _, m := range matches {
		lineMatches[m.Line] = append(lineMatches[m.Line], m)
	}

	// Process each line's matches from right to left
	lines := strings.Split(content, "\n")
	for lineNum, lineVersions := range lineMatches {
		if lineNum > len(lines) {
			continue
		}

		// Sort matches on this line by start position (descending)
		for i := 0; i < len(lineVersions)-1; i++ {
			for j := i + 1; j < len(lineVersions); j++ {
				if lineVersions[i].StartIndex < lineVersions[j].StartIndex {
					lineVersions[i], lineVersions[j] = lineVersions[j], lineVersions[i]
				}
			}
		}

		line := lines[lineNum-1]
		for _, match := range lineVersions {
			// Determine if we should include 'v' prefix based on the original
			versionToUse := newVersion
			if strings.Contains(match.FullMatch, "v"+match.Version) {
				versionToUse = "v" + newVersion
			}

			// Replace the version in the line
			if match.StartIndex >= 0 && match.EndIndex <= len(line) && match.StartIndex < match.EndIndex {
				before := line[:match.StartIndex]
				after := line[match.EndIndex:]

				// Reconstruct the version part maintaining the original format
				newVersionPart := match.Prefix + versionToUse + match.Suffix

				line = before + newVersionPart + after
			}
		}
		lines[lineNum-1] = line
	}

	// Write back to file
	output := strings.Join(lines, "\n")
	return os.WriteFile(filePath, []byte(output), 0644)
}

// BumpVersionInFile finds and replaces the main version in a file.
// When a file contains multiple version numbers (e.g., a package.json with dependencies),
// this function intelligently selects only the primary version field to update:
// - For JSON files: Updates only the top-level "version" field
// - For TOML files: Updates only the version in the [package] section
// - For other files: Updates the first version found
// This prevents accidentally bumping dependency versions or other unrelated version numbers.
func BumpVersionInFile(filePath string, newVersion string) (bool, error) {
	// Try to find the main version first
	mainMatch, err := FindMainVersionInFile(filePath)
	if err != nil {
		return false, err
	}

	if mainMatch == nil {
		return false, nil
	}

	// Replace only the main version
	if err := ReplaceVersionInFile(filePath, newVersion, []VersionMatch{*mainMatch}); err != nil {
		return false, err
	}

	return true, nil
}

// BumpAllVersionsInFile finds and replaces all versions in a file
func BumpAllVersionsInFile(filePath string, newVersion string) (bool, error) {
	matches, err := FindVersionsInFile(filePath)
	if err != nil {
		return false, err
	}

	if len(matches) == 0 {
		return false, nil
	}

	// Replace all found versions
	if err := ReplaceVersionInFile(filePath, newVersion, matches); err != nil {
		return false, err
	}

	return true, nil
}

// ScanVersionInFile performs a dry-run scan to find versions without modifying the file
func ScanVersionInFile(filePath string) ([]VersionMatch, error) {
	return FindVersionsInFile(filePath)
}
