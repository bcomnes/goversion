package goversion

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ExampleBumpVersionInFile demonstrates how to use the bump-in functionality
// to update version numbers in arbitrary files like package.json, README.md,
// or extension.toml files.
func ExampleBumpVersionInFile() {
	// Create a temporary directory for the example
	tmpDir, err := os.MkdirTemp("", "goversion_bumpin_example")
	if err != nil {
		fmt.Println("failed to create temporary directory:", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	// Create a package.json file with a version
	packageJSONPath := filepath.Join(tmpDir, "package.json")
	packageContent := `{
  "name": "example-app",
  "version": "1.0.0",
  "description": "Example application"
}`
	if err := os.WriteFile(packageJSONPath, []byte(packageContent), 0644); err != nil {
		fmt.Println("failed to write package.json:", err)
		return
	}

	// Create an extension.toml file with a version
	extensionTOMLPath := filepath.Join(tmpDir, "extension.toml")
	tomlContent := `[package]
name = "my-extension"
version = "1.0.0"
authors = ["Example Author"]`
	if err := os.WriteFile(extensionTOMLPath, []byte(tomlContent), 0644); err != nil {
		fmt.Println("failed to write extension.toml:", err)
		return
	}

	// Bump the version to 1.1.0 in package.json
	updated, err := BumpVersionInFile(packageJSONPath, "1.1.0")
	if err != nil {
		fmt.Println("failed to bump version in package.json:", err)
		return
	}
	if !updated {
		fmt.Println("no version found in package.json")
		return
	}

	// Bump the version to 1.1.0 in extension.toml
	updated, err = BumpVersionInFile(extensionTOMLPath, "1.1.0")
	if err != nil {
		fmt.Println("failed to bump version in extension.toml:", err)
		return
	}
	if !updated {
		fmt.Println("no version found in extension.toml")
		return
	}

	// Read and print the updated package.json
	packageResult, _ := os.ReadFile(packageJSONPath)
	fmt.Println("Updated package.json:")
	fmt.Println(string(packageResult))

	fmt.Println()

	// Read and print the updated extension.toml
	tomlResult, _ := os.ReadFile(extensionTOMLPath)
	fmt.Println("Updated extension.toml:")
	fmt.Println(string(tomlResult))

	// Output:
	// Updated package.json:
	// {
	//   "name": "example-app",
	//   "version": "1.1.0",
	//   "description": "Example application"
	// }
	//
	// Updated extension.toml:
	// [package]
	// name = "my-extension"
	// version = "1.1.0"
	// authors = ["Example Author"]
}

// ExampleScanVersionInFile demonstrates how to scan a file for version numbers
// without modifying it, useful for dry-run operations.
func ExampleScanVersionInFile() {
	// Create a temporary file with multiple version references
	tmpDir, err := os.MkdirTemp("", "goversion_scan_example")
	if err != nil {
		fmt.Println("failed to create temporary directory:", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	// Create a README.md with multiple version references
	readmePath := filepath.Join(tmpDir, "README.md")
	readmeContent := `# My Project

Current version: v2.0.0

## Installation

Install version 2.0.0 with:
` + "```bash" + `
npm install my-project@2.0.0
` + "```" + `

## Changelog

### Version 2.0.0
- Major release

### Version 1.0.0
- Initial release`

	if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
		fmt.Println("failed to write README.md:", err)
		return
	}

	// Scan for versions
	matches, err := ScanVersionInFile(readmePath)
	if err != nil {
		fmt.Println("failed to scan file:", err)
		return
	}

	// Print found versions
	fmt.Printf("Found %d version references:\n", len(matches))
	for i, match := range matches {
		fmt.Printf("%d. Line %d: version %s (pattern: %s)\n",
			i+1, match.Line, match.Version, match.Pattern.Name)
	}

	// Output:
	// Found 6 version references:
	// 1. Line 3: version 2.0.0 (pattern: VERSION assignment)
	// 2. Line 3: version 2.0.0 (pattern: current version text)
	// 3. Line 7: version 2.0.0 (pattern: install version text)
	// 4. Line 9: version 2.0.0 (pattern: at version)
	// 5. Line 14: version 2.0.0 (pattern: markdown version header)
	// 6. Line 17: version 1.0.0 (pattern: markdown version header)
}

// ExampleRun_withBumpIn demonstrates using the Run function with bump-in files
// to update versions in multiple files as part of a version bump operation.
func ExampleRun_withBumpIn() {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "goversion_run_bumpin_example")
	if err != nil {
		fmt.Println("failed to create temporary directory:", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Println("failed to initialize git repository:", string(output), err)
		return
	}

	// Configure git
	configCmds := [][]string{
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test User"},
	}
	for _, args := range configCmds {
		cmd = exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Println("failed to configure git:", string(output), err)
			return
		}
	}

	// Change to temp directory
	origDir, err := os.Getwd()
	if err != nil {
		fmt.Println("failed to get current directory:", err)
		return
	}
	if err := os.Chdir(tmpDir); err != nil {
		fmt.Println("failed to change directory:", err)
		return
	}
	defer os.Chdir(origDir)

	// Create version.go
	versionFile := filepath.Join(tmpDir, "version.go")
	if err := writeVersionFile(versionFile, "1.0.0"); err != nil {
		fmt.Println("failed to write version file:", err)
		return
	}

	// Create package.json
	packageFile := filepath.Join(tmpDir, "package.json")
	packageContent := `{
  "name": "example",
  "version": "1.0.0"
}`
	if err := os.WriteFile(packageFile, []byte(packageContent), 0644); err != nil {
		fmt.Println("failed to write package.json:", err)
		return
	}

	// Stage and commit initial files
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Println("failed to stage files:", string(output), err)
		return
	}

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Println("failed to commit:", string(output), err)
		return
	}

	// Run goversion with bump-in files
	meta, err := Run(versionFile, "minor", []string{versionFile}, []string{packageFile})
	if err != nil {
		fmt.Println("error bumping version:", err)
		return
	}

	// Print results
	fmt.Printf("Version bumped from %s to %s\n", meta.OldVersion, meta.NewVersion)
	fmt.Printf("Bump type: %s\n", meta.BumpType)
	// Print just the base filenames to avoid absolute path issues in tests
	var baseFiles []string
	for _, f := range meta.UpdatedFiles {
		baseFiles = append(baseFiles, filepath.Base(f))
	}
	fmt.Printf("Updated files: %v\n", baseFiles)

	// Read updated package.json
	updatedPackage, _ := os.ReadFile(packageFile)
	fmt.Println("\nUpdated package.json:")
	fmt.Println(string(updatedPackage))

	// Output:
	// Version bumped from 1.0.0 to 1.1.0
	// Bump type: minor
	// Updated files: [version.go package.json]
	//
	// Updated package.json:
	// {
	//   "name": "example",
	//   "version": "1.1.0"
	// }
}
