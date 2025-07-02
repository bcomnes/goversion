package goversion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindVersionsInFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []struct {
			version string
			line    int
		}
	}{
		{
			name: "package.json",
			content: `{
  "name": "my-app",
  "version": "1.2.3",
  "description": "Test app"
}`,
			expected: []struct {
				version string
				line    int
			}{
				{version: "1.2.3", line: 3},
			},
		},
		{
			name: "README.md",
			content: `# My Project

Version: 2.0.0

## Installation

Current version is v2.0.0`,
			expected: []struct {
				version string
				line    int
			}{
				{version: "2.0.0", line: 3},
				{version: "2.0.0", line: 7},
			},
		},
		{
			name: "config.yaml",
			content: `app:
  name: MyApp
  version: "3.1.4"
  port: 8080`,
			expected: []struct {
				version string
				line    int
			}{
				{version: "3.1.4", line: 3},
			},
		},
		{
			name: "setup.py",
			content: `from setuptools import setup

setup(
    name="mypackage",
    version="0.1.0",
    author="Test Author"
)`,
			expected: []struct {
				version string
				line    int
			}{
				{version: "0.1.0", line: 5},
			},
		},
		{
			name: "pom.xml",
			content: `<project>
  <groupId>com.example</groupId>
  <artifactId>my-app</artifactId>
  <version>1.0.0</version>
</project>`,
			expected: []struct {
				version string
				line    int
			}{
				{version: "1.0.0", line: 4},
			},
		},
		{
			name: "VERSION file",
			content: `VERSION=4.5.6`,
			expected: []struct {
				version string
				line    int
			}{
				{version: "4.5.6", line: 1},
			},
		},
		{
			name: "multiple versions",
			content: `# Changelog

## Version 2.1.0

### Features
- Added new feature

## Version 2.0.0

### Breaking Changes
- Changed API

Current version: v2.1.0`,
			expected: []struct {
				version string
				line    int
			}{
				{version: "2.1.0", line: 3},
				{version: "2.0.0", line: 8},
				{version: "2.1.0", line: 13},
				{version: "2.1.0", line: 13},
			},
		},
		{
			name: "prerelease version",
			content: `{
  "version": "1.2.3-beta.1"
}`,
			expected: []struct {
				version string
				line    int
			}{
				{version: "1.2.3-beta.1", line: 2},
			},
		},
		{
			name: "extension.toml",
			content: `[package]
name = "my-extension"
version = "0.5.0"
authors = ["Test Author"]

[dependencies]
some-lib = "1.0"`,
			expected: []struct {
				version string
				line    int
			}{
				{version: "0.5.0", line: 3},
			},
		},
		{
			name: "Cargo.toml",
			content: `[package]
name = "rust-project"
version = "2.1.0-alpha.2"
edition = "2021"`,
			expected: []struct {
				version string
				line    int
			}{
				{version: "2.1.0-alpha.2", line: 3},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpDir, err := os.MkdirTemp("", "version_bumper_test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tmpDir)

			tmpFile := filepath.Join(tmpDir, "test_file")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Find versions
			matches, err := FindVersionsInFile(tmpFile)
			if err != nil {
				t.Fatalf("FindVersionsInFile failed: %v", err)
			}

			// Check results
			if len(matches) != len(tt.expected) {
				t.Errorf("expected %d matches, got %d", len(tt.expected), len(matches))
				for i, m := range matches {
					t.Logf("Match %d: version=%s, line=%d", i, m.Version, m.Line)
				}
				return
			}

			for i, expected := range tt.expected {
				if i >= len(matches) {
					break
				}
				if matches[i].Version != expected.version {
					t.Errorf("match %d: expected version %s, got %s", i, expected.version, matches[i].Version)
				}
				if matches[i].Line != expected.line {
					t.Errorf("match %d: expected line %d, got %d", i, expected.line, matches[i].Line)
				}
			}
		})
	}
}

func TestBumpVersionInFile(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		newVersion  string
		expected    string
		shouldBump  bool
	}{
		{
			name: "package.json",
			content: `{
  "name": "my-app",
  "version": "1.2.3",
  "description": "Test app"
}`,
			newVersion: "2.0.0",
			expected: `{
  "name": "my-app",
  "version": "2.0.0",
  "description": "Test app"
}`,
			shouldBump: true,
		},
		{
			name: "README with v prefix",
			content: `# My Project

Current version: v1.0.0

Install with: npm install myproject@v1.0.0`,
			newVersion: "1.1.0",
			expected: `# My Project

Current version: v1.1.0

Install with: npm install myproject@v1.1.0`,
			shouldBump: true,
		},
		{
			name: "no version found",
			content: `# My Project

This is a project without any version numbers.`,
			newVersion:  "1.0.0",
			expected:    "", // Content should remain unchanged
			shouldBump:  false,
		},
		{
			name: "mixed formats",
			content: `app:
  version: "1.2.3"

# Version 1.2.3
VERSION=1.2.3`,
			newVersion: "2.0.0",
			expected: `app:
  version: "2.0.0"

# Version 2.0.0
VERSION=2.0.0`,
			shouldBump: true,
		},
		{
			name: "prerelease to release",
			content: `{
  "version": "1.0.0-beta.3"
}`,
			newVersion: "1.0.0",
			expected: `{
  "version": "1.0.0"
}`,
			shouldBump: true,
		},
		{
			name: "extension.toml",
			content: `[package]
name = "my-extension"
version = "0.5.0"
authors = ["Test Author"]

[dependencies]
some-lib = "1.0"`,
			newVersion: "1.0.0",
			expected: `[package]
name = "my-extension"
version = "1.0.0"
authors = ["Test Author"]

[dependencies]
some-lib = "1.0"`,
			shouldBump: true,
		},
		{
			name: "TOML with v prefix",
			content: `[package]
name = "project"
version = "v1.2.3"`,
			newVersion: "2.0.0",
			expected: `[package]
name = "project"
version = "v2.0.0"`,
			shouldBump: true,
		},
		{
			name: "multiple versions - only main updated",
			content: `{
  "name": "my-app",
  "version": "1.0.0",
  "dependencies": {
    "some-lib": "2.3.4",
    "another-lib": "5.6.7"
  }
}`,
			newVersion: "1.1.0",
			expected: `{
  "name": "my-app",
  "version": "1.1.0",
  "dependencies": {
    "some-lib": "2.3.4",
    "another-lib": "5.6.7"
  }
}`,
			shouldBump: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpDir, err := os.MkdirTemp("", "bump_version_test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tmpDir)

			tmpFile := filepath.Join(tmpDir, "test_file")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Bump version
			bumped, err := BumpVersionInFile(tmpFile, tt.newVersion)
			if err != nil {
				t.Fatalf("BumpVersionInFile failed: %v", err)
			}

			if bumped != tt.shouldBump {
				t.Errorf("expected bumped=%v, got %v", tt.shouldBump, bumped)
			}

			// Read result
			result, err := os.ReadFile(tmpFile)
			if err != nil {
				t.Fatal(err)
			}

			// Compare result
			if tt.shouldBump {
				if string(result) != tt.expected {
					t.Errorf("unexpected result:\nGot:\n%s\nExpected:\n%s", string(result), tt.expected)
				}
			} else {
				// Content should be unchanged
				if string(result) != tt.content {
					t.Errorf("content should be unchanged but was modified:\nOriginal:\n%s\nGot:\n%s", tt.content, string(result))
				}
			}
		})
	}
}

func TestVersionPatternMatching(t *testing.T) {
	// Test individual patterns
	tests := []struct {
		name    string
		input   string
		pattern VersionPattern
		matches bool
		version string
	}{
		{
			name:    "version assignment lowercase",
			input:   `version = "1.2.3"`,
			pattern: CommonVersionPatterns[4],
			matches: true,
			version: "1.2.3",
		},
		{
			name:    "VERSION assignment uppercase",
			input:   `VERSION: 2.0.0`,
			pattern: CommonVersionPatterns[1],
			matches: true,
			version: "2.0.0",
		},
		{
			name:    "JSON version field",
			input:   `"version": "3.1.4"`,
			pattern: CommonVersionPatterns[0],
			matches: true,
			version: "3.1.4",
		},
		{
			name:    "doc comment",
			input:   `@version 1.0.0-alpha`,
			pattern: CommonVersionPatterns[2],
			matches: true,
			version: "1.0.0-alpha",
		},
		{
			name:    "XML version tag",
			input:   `<version>4.5.6</version>`,
			pattern: CommonVersionPatterns[3],
			matches: true,
			version: "4.5.6",
		},
		{
			name:    "version with v prefix",
			input:   `version: "v1.2.3"`,
			pattern: CommonVersionPatterns[4],
			matches: true,
			version: "1.2.3",
		},
		{
			name:    "TOML version field",
			input:   `version = "1.2.3"`,
			pattern: CommonVersionPatterns[9],
			matches: true,
			version: "1.2.3",
		},
		{
			name:    "install version pattern",
			input:   `Install version 1.2.3 with npm`,
			pattern: CommonVersionPatterns[8],
			matches: true,
			version: "1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := tt.pattern.Pattern.FindStringSubmatch(tt.input)
			if tt.matches && matches == nil {
				t.Errorf("expected pattern to match but it didn't")
			}
			if !tt.matches && matches != nil {
				t.Errorf("expected pattern not to match but it did")
			}
			if tt.matches && matches != nil {
				// Find the version in the match groups
				found := false
				for _, group := range matches[1:] {
					cleaned := strings.TrimPrefix(group, "v")
					if cleaned == tt.version {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected to find version %s in matches %v", tt.version, matches)
				}
			}
		})
	}
}

func TestFindMainVersionInFile(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		expectedVersion string
		shouldFind      bool
	}{
		{
			name: "package.json with dependencies",
			content: `{
  "name": "my-app",
  "version": "1.2.3",
  "dependencies": {
    "react": "18.0.0",
    "express": "4.18.0"
  },
  "devDependencies": {
    "jest": "29.0.0"
  }
}`,
			expectedVersion: "1.2.3",
			shouldFind:      true,
		},
		{
			name: "TOML with multiple sections",
			content: `[package]
name = "my-crate"
version = "0.5.0"

[dependencies]
serde = { version = "1.0", features = ["derive"] }
tokio = { version = "1.0", features = ["full"] }

[dev-dependencies]
criterion = { version = "0.4", features = ["html_reports"] }`,
			expectedVersion: "0.5.0",
			shouldFind:      true,
		},
		{
			name: "nested JSON version",
			content: `{
  "config": {
    "version": "2.0.0"
  },
  "dependencies": {
    "lib": "1.0.0"
  }
}`,
			expectedVersion: "2.0.0",
			shouldFind:      true,
		},
		{
			name: "VERSION file",
			content: `VERSION=3.2.1`,
			expectedVersion: "3.2.1",
			shouldFind:      true,
		},
		{
			name: "no version",
			content: `{
  "name": "no-version-app",
  "description": "App without version"
}`,
			expectedVersion: "",
			shouldFind:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpDir, err := os.MkdirTemp("", "find_main_version_test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tmpDir)

			tmpFile := filepath.Join(tmpDir, "test_file")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Find main version
			match, err := FindMainVersionInFile(tmpFile)
			if err != nil {
				t.Fatalf("FindMainVersionInFile failed: %v", err)
			}

			if tt.shouldFind {
				if match == nil {
					t.Errorf("expected to find version %s, but found none", tt.expectedVersion)
				} else if match.Version != tt.expectedVersion {
					t.Errorf("expected version %s, got %s", tt.expectedVersion, match.Version)
				}
			} else {
				if match != nil {
					t.Errorf("expected no version, but found %s", match.Version)
				}
			}
		})
	}
}

func TestScanVersionInFile(t *testing.T) {
	// Create temporary file
	tmpDir, err := os.MkdirTemp("", "scan_version_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	content := `{
  "name": "test-app",
  "version": "1.0.0",
  "scripts": {
    "start": "node index.js"
  }
}`

	tmpFile := filepath.Join(tmpDir, "package.json")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Scan for versions
	matches, err := ScanVersionInFile(tmpFile)
	if err != nil {
		t.Fatalf("ScanVersionInFile failed: %v", err)
	}

	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}

	if len(matches) > 0 && matches[0].Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", matches[0].Version)
	}
}
