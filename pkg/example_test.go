package goversion

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ExampleRun demonstrates how to use the Run function in a Git repository.
// It creates a temporary directory, initializes a Git repo, writes an initial
// version file (using escaped newline literals), stages and commits it, then
// changes the current working directory to that temporary repo before bumping
// the version with a "patch" directive (bumping 1.2.3 to 1.2.4). The updated file
// content is then printed out.
func ExampleRun() {
	// Create a temporary directory.
	tmpDir, err := os.MkdirTemp("", "goversion_example")
	if err != nil {
		fmt.Println("failed to create temporary directory:", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	// Initialize a new Git repository in tmpDir.
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Println("failed to initialize git repository:", string(output), err)
		return
	}

	// Configure Git user settings.
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

	// Change working directory to the temporary repository so that Git commands run correctly.
	origDir, err := os.Getwd()
	if err != nil {
		fmt.Println("failed to get current working directory:", err)
		return
	}
	if err := os.Chdir(tmpDir); err != nil {
		fmt.Println("failed to change working directory:", err)
		return
	}
	defer os.Chdir(origDir) // Restore original working directory when done.

	// Define the path to the version file.
	versionFile := filepath.Join(tmpDir, "version.go")

	// Write an initial version file using escaped newline literals.
	initialContent := "package version\n\nvar (\n\tVersion = \"1.2.3\"\n)\n"
	err = os.WriteFile(versionFile, []byte(initialContent), 0644)
	if err != nil {
		fmt.Println("failed to write version file:", err)
		return
	}

	// Stage and commit the initial version file.
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Println("failed to execute git add:", string(output), err)
		return
	}
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Println("failed to execute git commit:", string(output), err)
		return
	}

	// Call Run to bump the version ("patch" will bump 1.2.3 to 1.2.4).
	_, err = Run(versionFile, "patch", []string{versionFile}, []string{})
	if err != nil {
		fmt.Println("error bumping version:", err)
		return
	}

	// Read the updated version file.
	newContent, err := os.ReadFile(versionFile)
	if err != nil {
		fmt.Println("failed to read version file:", err)
		return
	}

	// Print the updated content.
	fmt.Printf("%s", newContent)

	// Output:
	// package version
	//
	// var (
	// 	Version = "1.2.4"
	// )
}
