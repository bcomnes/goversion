package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionCommandWorkdirDryRun(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "go")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/acme/repo/go\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "version.go"), []byte("package tool\n\nvar Version = \"1.2.3\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test User"}, {"add", "."}, {"commit", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}

	var output, errorOutput bytes.Buffer
	code := runVersionCommand([]string{"-workdir", moduleDir, "-dry", "patch"}, &output, &errorOutput)
	if code != 0 {
		t.Fatalf("runVersionCommand returned %d: %s", code, errorOutput.String())
	}
	for _, want := range []string{"New Version: 1.2.4", "Tag:         go/v1.2.4"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
	contents, err := os.ReadFile(filepath.Join(moduleDir, "version.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), fmt.Sprintf("%q", "1.2.3")) {
		t.Fatalf("dry run changed version.go: %s", contents)
	}
}
