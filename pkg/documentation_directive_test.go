package goversion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteDocumentationTextIgnoreNextLine(t *testing.T) {
	const old = "example.com/acme/widget"
	const next = old + "/v2"
	const directive = "<!-- goversion:ignore-next-line -->"
	refs := "\t`" + old + "`  " + old + "/client@latest https://pkg.go.dev/" + old + "/client#Overview  "
	updated := strings.ReplaceAll(refs, old, next)
	for _, tc := range []struct{ name, input, want string }{
		{"one physical line", refs + "\n" + directive + "\n" + refs + "\n" + refs, updated + "\n" + directive + "\n" + refs + "\n" + updated},
		{"whitespace", " \t" + directive + "\t  \n" + refs, " \t" + directive + "\t  \n" + refs},
		{"blank consumes skip", directive + "\n\n" + refs, directive + "\n\n" + updated},
		{"whitespace line consumes skip", directive + "\n \t\n" + refs, directive + "\n \t\n" + updated},
		{"consecutive directives", directive + "\n" + directive + "\n" + refs + "\n" + refs, directive + "\n" + directive + "\n" + refs + "\n" + updated},
		{"prose prefix", "Use " + directive + "\n" + refs, "Use " + directive + "\n" + updated},
		{"prose suffix", directive + " is documented here\n" + refs, directive + " is documented here\n" + updated},
		{"bare mention", "goversion:ignore-next-line\n" + refs, "goversion:ignore-next-line\n" + updated},
		{"directive at EOF", refs + "\n" + directive, updated + "\n" + directive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			documentationDirectiveLineEndings(t, tc.input, tc.want, func(input string) string {
				return rewriteDocumentationText(input, old, next)
			})
		})
	}
}

func TestRewriteGoDocumentationIgnoreNextLine(t *testing.T) {
	const old = "example.com/acme/widget"
	const next = old + "/v2"
	const directive = "// goversion:ignore-next-line"
	refs := "// café " + old + "  " + old + "/client@latest https://pkg.go.dev/" + old + "#Overview  "
	updated := strings.ReplaceAll(refs, old, next)
	for _, tc := range []struct{ name, input, want string }{
		{"per comment not group", refs + "\n" + directive + "\n" + refs + "\n" + refs, updated + "\n" + directive + "\n" + refs + "\n" + updated},
		{"whitespace", " \t//\tgoversion:ignore-next-line \t\n" + refs, " \t//\tgoversion:ignore-next-line \t\n" + refs},
		{"no space after slashes", "//goversion:ignore-next-line\n" + refs, "//goversion:ignore-next-line\n" + refs},
		{"blank consumes skip", directive + "\n\n" + refs, directive + "\n\n" + updated},
		{"whitespace line consumes skip", directive + "\n \t\n" + refs, directive + "\n \t\n" + updated},
		{"consecutive directives", directive + "\n" + directive + "\n" + refs + "\n" + refs, directive + "\n" + directive + "\n" + refs + "\n" + updated},
		{"code consumes skip", directive + "\nvar N = 1\n" + refs, directive + "\nvar N = 1\n" + updated},
		{"next line trailing comment", directive + "\nvar N = 1 " + refs + "\n" + refs, directive + "\nvar N = 1 " + refs + "\n" + updated},
		{"prose prefix", "// Use goversion:ignore-next-line\n" + refs, "// Use goversion:ignore-next-line\n" + updated},
		{"prose suffix", directive + " is documented here\n" + refs, directive + " is documented here\n" + updated},
		{"non standalone trailing directive", "var N = 1 " + directive + "\n" + refs, "var N = 1 " + directive + "\n" + updated},
		{"non standalone after block comment", "/* note */ " + directive + "\n" + refs, "/* note */ " + directive + "\n" + updated},
		{"raw string directive", "var Raw = `\n" + directive + "\n` " + refs + "\n" + refs, "var Raw = `\n" + directive + "\n` " + updated + "\n" + updated},
		{"imports untouched by doc scanner", directive + "\nimport _ \"" + old + "/client\"\n" + refs, directive + "\nimport _ \"" + old + "/client\"\n" + updated},
		{"ordinary import untouched", "import _ \"" + old + "/client\"\n" + refs, "import _ \"" + old + "/client\"\n" + updated},
		{"directive at EOF", refs + "\n" + directive, updated + "\n" + directive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			documentationDirectiveLineEndings(t, "package widget\n\n"+tc.input, "package widget\n\n"+tc.want, func(input string) string {
				got, err := rewriteGoDocumentation("directive.go", []byte(input), old, next)
				if err != nil {
					t.Fatalf("rewriteGoDocumentation: %v", err)
				}
				return string(got)
			})
		})
	}
}

func documentationDirectiveLineEndings(t *testing.T, input, want string, rewrite func(string) string) {
	t.Helper()
	for _, ending := range []struct{ name, value string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		for _, final := range []struct{ name, value string }{{"final newline", "\n"}, {"no final newline", ""}} {
			t.Run(ending.name+"/"+final.name, func(t *testing.T) {
				before := strings.ReplaceAll(input+final.value, "\n", ending.value)
				expected := strings.ReplaceAll(want+final.value, "\n", ending.value)
				if got := rewrite(before); got != expected {
					t.Errorf("rewrite = %q; want %q", got, expected)
				}
			})
		}
	}
}

func TestDocumentationIgnoreNextLineMajorIntegration(t *testing.T) {
	if err := checkGit(); err != nil {
		t.Skip("git is not available on system")
	}
	const old = "example.com/acme/repo/tools/widget"
	const next = old + "/v2"
	root, dir := initNestedModuleRepo(t, old, "1.9.0")
	readme := filepath.Join(dir, "README.md")
	consumer := filepath.Join(dir, "consumer.go")
	protectedMarkdown := filepath.Join(dir, "protected.markdown")
	protectedGo := filepath.Join(dir, "protected.go")
	mod := filepath.Join(dir, "go.mod")
	original := map[string]string{
		readme:            "<!-- goversion:ignore-next-line -->\n" + old + " " + old + "/client@latest\nSee " + old + ".\n",
		consumer:          "package widget\n\n// goversion:ignore-next-line\nimport _ \"" + old + "/client\"\n\n// goversion:ignore-next-line\n// " + old + " " + old + "/client\n// Ordinary " + old + ".\n",
		protectedMarkdown: " \t<!-- goversion:ignore-next-line --> \t\r\n" + old + " https://pkg.go.dev/" + old,
		protectedGo:       "package widget\r\n\r\n//goversion:ignore-next-line\r\n// " + old + " " + old + "/client",
		mod:               "// goversion:ignore-next-line\nmodule " + old + "\n\ngo 1.23\n",
	}
	for path, content := range original {
		documentationWrite(t, path, content)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "add ignore-next-line fixtures")
	options := VersionOptions{WorkDir: dir}
	dry, err := DryRunWithOptions(options, "major")
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{mod, filepath.Join(dir, "version.go"), readme, consumer}
	documentationAssertPaths(t, dry.UpdatedFiles, wantPaths)
	for path, content := range original {
		documentationAssertContent(t, path, content)
	}
	if status := gitOutput(t, root, "--no-pager", "status", "--porcelain"); status != "" {
		t.Fatalf("dry run changed worktree/index: %s", status)
	}
	wet, err := RunWithOptions(options, "major")
	if err != nil {
		t.Fatal(err)
	}
	documentationAssertPaths(t, wet.UpdatedFiles, wantPaths)
	documentationAssertPaths(t, wet.UpdatedFiles, dry.UpdatedFiles)
	documentationAssertContent(t, readme, strings.Replace(original[readme], "See "+old, "See "+next, 1))
	wantConsumer := strings.Replace(original[consumer], "import _ \""+old, "import _ \""+next, 1)
	wantConsumer = strings.Replace(wantConsumer, "// Ordinary "+old, "// Ordinary "+next, 1)
	documentationAssertContent(t, consumer, wantConsumer)
	for _, path := range []string{protectedMarkdown, protectedGo} {
		documentationAssertContent(t, path, original[path])
	}
	data, err := os.ReadFile(mod)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "module "+next+"\n") {
		t.Errorf("go.mod directive must not prevent module migration: %s", data)
	}
}
