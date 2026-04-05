package codegraph

import (
	"os"
	"path/filepath"
	"strings"
)

// extensionToLanguage maps file extensions to language identifiers.
var extensionToLanguage = map[string]string{
	".go":    "go",
	".py":    "python",
	".pyi":   "python",
	".js":    "javascript",
	".jsx":   "javascript",
	".mjs":   "javascript",
	".ts":    "typescript",
	".tsx":   "typescript",
	".rs":    "rust",
	".java":  "java",
	".kt":    "kotlin",
	".rb":    "ruby",
	".php":   "php",
	".c":     "c",
	".h":     "c",
	".cpp":   "cpp",
	".hpp":   "cpp",
	".cc":    "cpp",
	".cs":    "csharp",
	".swift": "swift",
	".scala": "scala",
	".ex":    "elixir",
	".exs":   "elixir",
	".sh":    "shell",
	".bash":  "shell",
	".zsh":   "shell",
	".css":   "css",
	".scss":  "css",
	".html":  "html",
	".htm":   "html",
	".yaml":  "yaml",
	".yml":   "yaml",
	".json":  "json",
	".toml":  "toml",
	".md":    "markdown",
	".sql":   "sql",
	".lua":   "lua",
	".r":     "r",
	".R":     "r",
	".dart":  "dart",
	".zig":   "zig",
}

// indexableLanguages lists languages that have parser support (Go AST or regex).
var indexableLanguages = map[string]bool{
	"go":         true,
	"python":     true,
	"javascript": true,
	"typescript": true,
	"rust":       true,
}

// manifestToLanguage maps manifest files to their primary language.
var manifestToLanguage = map[string]string{
	"go.mod":           "go",
	"go.sum":           "go",
	"package.json":     "javascript",
	"tsconfig.json":    "typescript",
	"Cargo.toml":       "rust",
	"pyproject.toml":   "python",
	"setup.py":         "python",
	"requirements.txt": "python",
	"Gemfile":          "ruby",
	"pom.xml":          "java",
	"build.gradle":     "java",
}

// DetectLanguage returns the language for a file based on its extension.
// Returns empty string if the language is not recognized.
func DetectLanguage(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	return extensionToLanguage[ext]
}

// DetectProjectLanguage determines the primary language of a project
// by checking for manifest files in the given directory.
func DetectProjectLanguage(dir string) string {
	for manifest, lang := range manifestToLanguage {
		path := filepath.Join(dir, manifest)
		if _, err := os.Stat(path); err == nil {
			return lang
		}
	}
	return ""
}

// IsIndexableFile returns true if the file's language has parser support.
func IsIndexableFile(filename string) bool {
	lang := DetectLanguage(filename)
	return indexableLanguages[lang]
}

// skipDirs lists directories that should always be excluded from indexing.
var skipDirs = map[string]bool{
	"node_modules":  true,
	"venv":          true,
	".venv":         true,
	"vendor":        true,
	".git":          true,
	"__pycache__":   true,
	".cache":        true,
	".tox":          true,
	".mypy_cache":   true,
	".pytest_cache": true,
	"dist":          true,
	"build":         true,
	".celeste":      true,
	".next":         true,
	".nuxt":         true,
	"target":        true,
	"env":           true,
	"bin":           true,
	"obj":           true,
	"testdata":      true,
}

// ShouldSkipPath returns true if the path should be excluded from indexing.
func ShouldSkipPath(path string) bool {
	// The root directory "." should not be skipped
	if path == "." {
		return false
	}

	base := filepath.Base(path)

	// Skip hidden directories and files
	if strings.HasPrefix(base, ".") {
		return true
	}

	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts {
		if skipDirs[part] {
			return true
		}
	}

	return false
}

// GitignoreFilter holds compiled gitignore patterns for matching.
type GitignoreFilter struct {
	patterns []gitignorePattern
}

type gitignorePattern struct {
	pattern  string
	isDir    bool
	negation bool
}

// LoadGitignore reads a .gitignore file and returns a filter.
// Returns nil (no filter) if the file doesn't exist or can't be read.
func LoadGitignore(projectRoot string) *GitignoreFilter {
	data, err := os.ReadFile(filepath.Join(projectRoot, ".gitignore"))
	if err != nil {
		return nil
	}

	var patterns []gitignorePattern
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := gitignorePattern{}
		if strings.HasPrefix(line, "!") {
			p.negation = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			p.isDir = true
			line = strings.TrimSuffix(line, "/")
		}
		p.pattern = line
		patterns = append(patterns, p)
	}

	return &GitignoreFilter{patterns: patterns}
}

// ShouldSkip returns true if the given relative path should be ignored.
// isDir indicates whether the path is a directory.
func (f *GitignoreFilter) ShouldSkip(relPath string, isDir bool) bool {
	if f == nil {
		return false
	}

	matched := false
	base := filepath.Base(relPath)

	for _, p := range f.patterns {
		// Directory-only patterns skip files
		if p.isDir && !isDir {
			continue
		}

		// Try matching against the base name
		if ok, _ := filepath.Match(p.pattern, base); ok {
			matched = !p.negation
			continue
		}

		// Try matching against the full relative path
		if ok, _ := filepath.Match(p.pattern, filepath.ToSlash(relPath)); ok {
			matched = !p.negation
			continue
		}

		// Try matching path components for patterns like "somedir"
		if !strings.Contains(p.pattern, "/") {
			parts := strings.Split(filepath.ToSlash(relPath), "/")
			for _, part := range parts {
				if ok, _ := filepath.Match(p.pattern, part); ok {
					matched = !p.negation
					break
				}
			}
		}
	}

	return matched
}
