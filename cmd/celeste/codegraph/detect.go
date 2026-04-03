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

	// Skip common non-source directories
	skipDirs := map[string]bool{
		"node_modules": true,
		"vendor":       true,
		"__pycache__":  true,
		".git":         true,
		".celeste":     true,
		"dist":         true,
		"build":        true,
		"target":       true,
		"bin":          true,
		"obj":          true,
		"testdata":     true,
	}

	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts {
		if skipDirs[part] {
			return true
		}
	}

	return false
}
