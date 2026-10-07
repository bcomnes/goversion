package goversion

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestRunWithOptionsTracksIgnoredFilesOutsideWorkDir covers repository-wide tracked
// discovery and keeps automatic update reporting separate from extra-only staging.
func TestRunWithOptionsTracksIgnoredFilesOutsideWorkDir(t *testing.T) {
	for _, bump := range []bool{false, true} {
		name := "extra only"
		if bump {
			name = "bump and extra"
		}
		t.Run(name, func(t *testing.T) {
			root, workDir := initNestedModuleRepo(t, "example.com/acme/repo/tools/widget", "1.2.3")
			if err := os.Mkdir(filepath.Join(root, "release"), 0o755); err != nil {
				t.Fatal(err)
			}
			tracked := filepath.Join(root, "release", "version.txt")
			for path, content := range map[string]string{
				tracked:                           "1.2.3\n",
				filepath.Join(root, ".gitignore"): "/release/\n/build.txt\n",
			} {
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			gitRun(t, root, "add", ".gitignore")
			gitRun(t, root, "add", "-f", "release/version.txt")
			gitRun(t, root, "commit", "-m", "add tracked ignored release file")
			if err := os.WriteFile(filepath.Join(root, "build.txt"), []byte("untracked output\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			options := VersionOptions{
				WorkDir:    workDir,
				ExtraFiles: []string{"../../release/version.txt", "../../release/version.txt", "version.go"},
			}
			wantUpdated := []string{filepath.Join(workDir, "go.mod"), filepath.Join(workDir, "version.go")}
			wantRelease := "extra-only change"
			if bump {
				options.BumpFiles = []string{"../../release/version.txt", "../../release/version.txt"}
				wantUpdated = append(wantUpdated, tracked)
				wantRelease = "2.0.0"
			} else if err := os.WriteFile(tracked, []byte(wantRelease+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			meta, err := RunWithOptions(options, "major")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(meta.UpdatedFiles, wantUpdated) {
				t.Fatalf("UpdatedFiles = %v; want %v", meta.UpdatedFiles, wantUpdated)
			}
			if got := strings.TrimSpace(gitOutput(t, root, "show", "HEAD:release/version.txt")); got != wantRelease {
				t.Fatalf("committed release file = %q; want %q", got, wantRelease)
			}
			if got := strings.Fields(gitOutput(t, root, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")); !slices.Equal(got, []string{"release/version.txt", "tools/widget/go.mod", "tools/widget/version.go"}) {
				t.Fatalf("unexpected committed files: %v", got)
			}
			if got := gitOutput(t, root, "ls-files", "--", "build.txt"); got != "" {
				t.Fatalf("ignored untracked output was staged: %q", got)
			}
			if got := gitOutput(t, root, "status", "--porcelain"); got != "" {
				t.Fatalf("working tree is dirty after bump: %s", got)
			}
			if got := strings.TrimSpace(gitOutput(t, root, "rev-parse", meta.Tag)); got != strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD")) {
				t.Fatalf("tag %q does not point to the bump commit", meta.Tag)
			}
		})
	}
}
