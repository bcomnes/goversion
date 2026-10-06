package goversion

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"

	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/mod/module"
)

// Keep URL tokens whole so a module path inside an unrelated URL is not rewritten.
var documentationToken = regexp.MustCompile("https?://[^\\s<>\"'`]+|[^\\s<>\"'`()[\\]{}*|]+")
var documentationMajor = regexp.MustCompile(`^/v[0-9]+(?:/|$)`)

const documentationIgnoreNextLine = "goversion:ignore-next-line"

// rewriteDocumentationText updates Markdown references, honoring a standalone
// HTML ignore-next-line comment. Blank lines consume the directive too.
func rewriteDocumentationText(text, oldMod, newMod string, excludedModules ...string) string {
	var result strings.Builder
	ignore := false
	for _, line := range strings.SplitAfter(text, "\n") {
		directive := strings.TrimSpace(line) == "<!-- "+documentationIgnoreNextLine+" -->"
		if ignore || directive {
			result.WriteString(line)
		} else {
			result.WriteString(rewriteDocumentationLine(line, oldMod, newMod, excludedModules...))
		}
		ignore = directive
	}
	return result.String()
}

// rewriteDocumentationLine updates self module/package references while preserving
// unrelated URLs and explicit version pins. The moving @latest selector follows
// the new module path, unlike a pinned release such as @v1.2.3.
func rewriteDocumentationLine(text, oldMod, newMod string, excludedModules ...string) string {
	if oldMod == "" || oldMod == newMod {
		return text
	}
	_, oldMajor, _ := module.SplitPathVersion(oldMod)
	return documentationToken.ReplaceAllStringFunc(text, func(reference string) string {
		core := strings.TrimRight(reference, ".,;:!)]}*|")
		punctuation := reference[len(core):]
		prefix, target := "", core
		badge := false
		for _, host := range []string{"https://pkg.go.dev/badge/", "http://pkg.go.dev/badge/", "https://pkg.go.dev/", "http://pkg.go.dev/", "https://godoc.org/", "http://godoc.org/"} {
			if strings.HasPrefix(target, host) {
				prefix, target = host, strings.TrimPrefix(target, host)
				badge = strings.Contains(host, "/badge/")
				break
			}
		}
		// Anchors and query parameters belong to the URL, not the module path.
		path, suffix := target, ""
		if i := strings.IndexAny(path, "?#"); i >= 0 {
			path, suffix = target[:i], target[i:]
		}
		modulePath := strings.SplitN(path, "@", 2)[0]
		if badge {
			modulePath = strings.TrimSuffix(modulePath, ".svg")
		}
		if excludedModuleReference(modulePath, excludedModules) {
			return reference
		}
		if !strings.HasPrefix(path, oldMod) {
			return reference
		}
		if i := strings.IndexByte(path, '@'); i >= 0 {
			version := strings.SplitN(path[i+1:], "/", 2)[0]
			if badge {
				version = strings.TrimSuffix(version, ".svg")
			}
			if version != "latest" {
				return reference
			}
		}
		rest := strings.TrimPrefix(path, oldMod)
		if badge && rest == ".svg" {
			return prefix + newMod + rest + suffix + punctuation
		}
		if rest != "" && !strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, "@latest") {
			return reference
		}
		majorPath := strings.SplitN(rest, "@", 2)[0]
		if badge {
			majorPath = strings.TrimSuffix(majorPath, ".svg")
		}
		if oldMajor == "" && documentationMajor.MatchString(majorPath) {
			return reference
		}
		return prefix + newMod + rest + suffix + punctuation
	})
}

// rewriteGoDocumentation edits comment byte ranges only, leaving literals and
// executable code untouched. Work backwards so earlier offsets remain valid.
func rewriteGoDocumentation(filename string, data []byte, oldMod, newMod string, excludedModules ...string) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	text := string(data)
	ignoredLines := make(map[int]bool)
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if !strings.HasPrefix(comment.Text, "//") || strings.TrimSpace(comment.Text[2:]) != documentationIgnoreNextLine {
				continue
			}
			position := fset.PositionFor(comment.Pos(), false)
			lineStart := strings.LastIndexByte(text[:position.Offset], '\n') + 1
			if strings.TrimSpace(text[lineStart:position.Offset]) == "" {
				ignoredLines[position.Line+1] = true
			}
		}
	}
	for i := len(file.Comments) - 1; i >= 0; i-- {
		comments := file.Comments[i].List
		for j := len(comments) - 1; j >= 0; j-- {
			comment := comments[j]
			position := fset.PositionFor(comment.Pos(), false)
			start := position.Offset
			// Comment.End excludes carriage returns. Locate the end in the
			// original bytes instead to preserve CRLF files without truncation.
			end := len(data)
			if strings.HasPrefix(string(data[start:]), "/*") {
				end = start + strings.Index(string(data[start:]), "*/") + 2
			} else if n := strings.IndexByte(string(data[start:]), '\n'); n >= 0 {
				end = start + n
			}
			bodyStart, bodyEnd := start+2, end
			if data[start+1] == '*' {
				bodyEnd -= 2
			}
			var updated strings.Builder
			for n, line := range strings.SplitAfter(string(data[bodyStart:bodyEnd]), "\n") {
				if !ignoredLines[position.Line+n] {
					line = rewriteDocumentationLine(line, oldMod, newMod, excludedModules...)
				}
				updated.WriteString(line)
			}
			text = text[:bodyStart] + updated.String() + text[bodyEnd:]
		}
	}
	return []byte(text), nil
}

// updateDocumentationReferences shares discovery and transformation between dry
// and real runs. Only files owned by this module are considered; symlinks are not followed.
func updateDocumentationReferences(modDir, oldMod, newMod string, dryRun bool) ([]string, error) {
	if oldMod == newMod || oldMod == "" {
		return nil, nil
	}
	excludedModules, err := nestedModulePaths(modDir)
	if err != nil {
		return nil, err
	}
	var changed []string
	err = walkModuleFiles(modDir, func(filename string, entry fs.DirEntry) error {
		ext := strings.ToLower(filepath.Ext(filename))
		if ext != ".md" && ext != ".markdown" && ext != ".go" {
			return nil
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		var updated []byte
		if ext == ".go" {
			updated, err = rewriteGoDocumentation(filename, data, oldMod, newMod, excludedModules...)
		} else {
			updated = []byte(rewriteDocumentationText(string(data), oldMod, newMod, excludedModules...))
		}
		if err != nil {
			return fmt.Errorf("rewrite documentation in %s: %w", filename, err)
		}
		if string(data) == string(updated) {
			return nil
		}
		if !dryRun {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if err := os.WriteFile(filename, updated, info.Mode().Perm()); err != nil {
				return err
			}
		}
		changed = append(changed, filename)
		return nil
	})
	return changed, err
}

// uniquePaths removes duplicate file entries without changing discovery order.
func uniquePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	var unique []string
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			unique = append(unique, path)
		}
	}
	return unique
}
