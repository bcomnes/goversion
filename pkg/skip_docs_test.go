package goversion

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSkipDocsPreservesExplicitBumpsAndHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a shell post-bump hook")
	}
	const old = "example.com/acme/repo/tools/widget"
	root, dir := initNestedModuleRepo(t, old, "1.9.0")
	readme := filepath.Join(dir, "README.md")
	bumpFile := filepath.Join(dir, "release.md")
	hookFile := filepath.Join(dir, "hook.md")
	consumer := filepath.Join(dir, "consumer.go")
	goDoc := filepath.Join(dir, "doc.go")
	script := filepath.Join(dir, "hook.sh")
	original := map[string]string{
		readme:   "https://pkg.go.dev/" + old + "#Client\n",
		bumpFile: "Version 1.9.0\nhttps://pkg.go.dev/" + old + "\n",
		hookFile: "before hook\n",
		consumer: "package widget\nimport _ \"" + old + "/client\"\n// See " + old + ".\n",
		goDoc:    "// Package widget: https://pkg.go.dev/" + old + "\npackage widget\n",
		script:   "#!/bin/sh\nprintf 'hook ran\\n' > hook.md\n",
	}
	for path, content := range original {
		documentationWrite(t, path, content)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "add skip-docs fixtures")
	options := VersionOptions{
		WorkDir: dir, SkipDocs: true,
		BumpFiles: []string{"release.md"}, ExtraFiles: []string{"hook.md"}, PostBumpScript: "hook.sh",
	}
	dry, err := DryRunWithOptions(options, "major")
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{filepath.Join(dir, "go.mod"), filepath.Join(dir, "version.go"), consumer, bumpFile}
	documentationAssertPaths(t, dry.UpdatedFiles, wantPaths)
	for path, content := range original {
		documentationAssertContent(t, path, content)
	}
	wet, err := RunWithOptions(options, "major")
	if err != nil {
		t.Fatal(err)
	}
	documentationAssertPaths(t, wet.UpdatedFiles, wantPaths)
	documentationAssertContent(t, readme, original[readme])
	documentationAssertContent(t, goDoc, original[goDoc])
	documentationAssertContent(t, bumpFile, strings.Replace(original[bumpFile], "1.9.0", "2.0.0", 1))
	documentationAssertContent(t, hookFile, "hook ran\n")
	data, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\""+old+"/v2/client\"") || !strings.Contains(string(data), "// See "+old+".") {
		t.Fatalf("expected updated import and unchanged comment: %s", data)
	}
	if status := gitOutput(t, root, "--no-pager", "status", "--porcelain"); status != "" {
		t.Fatalf("uncommitted changes: %s", status)
	}
}
