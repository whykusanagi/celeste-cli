package codegraph

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
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
	".cxx":   "cpp",
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

// indexableLanguages lists languages that have parser support: Go AST,
// tree-sitter, or the regex fallback. Java, C, C++ and Ruby are parsed by
// tree-sitter in cgo builds; a CGO_ENABLED=0 build hands them to the
// generic regex parser, which extracts what it can and never errors.
var indexableLanguages = map[string]bool{
	"go":         true,
	"python":     true,
	"javascript": true,
	"typescript": true,
	"rust":       true,
	"php":        true,
	"java":       true,
	"c":          true,
	"cpp":        true,
	"ruby":       true,
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

// IsCppHeader reports whether relPath is a .h header whose content src is
// C++: it uses C++-only syntax (a class, namespace or template declaration,
// an access specifier, virtual, a :: scope, or a struct with a member
// function, constructor or destructor) outside comments, literals and
// attributes. Such a header is parsed with the C++ grammar; other .h
// headers stay C (review of #381). Its language stays "c" in the index: C
// and C++ resolve names as one family (languageFamily).
func IsCppHeader(relPath string, src []byte) bool {
	if !strings.EqualFold(filepath.Ext(relPath), ".h") {
		return false
	}
	src = cppCommentsAndLiterals.ReplaceAll(src, []byte(" "))
	// A C23 attribute ([[gnu::unused]]) has a "::" too; GNU attributes
	// hold calls that read as member functions.
	src = cAttributes.ReplaceAll(src, []byte(" "))
	if cppHeaderSyntax.Match(src) {
		return true
	}
	for _, m := range cStructBody.FindAllSubmatch(src, -1) {
		name, body := m[1], m[2]
		if cppMemberFunction.Match(body) {
			return true
		}
		if len(name) == 0 {
			continue
		}
		for _, c := range cBareCall.FindAllSubmatch(body, -1) {
			if bytes.Equal(c[1], name) {
				return true // a constructor
			}
		}
	}
	return false
}

// cBareCall matches a name and "(" at the start of a member declaration,
// with no type before it: a constructor when it is the struct's name.
var cBareCall = regexp.MustCompile(`(?:^|[\s;])([A-Za-z_]\w*)\s*\(`)

// cppHeaderSyntax matches C++-only syntax in a header.
var cppHeaderSyntax = regexp.MustCompile(`(?m)^\s*(?:class|namespace|template)\b|\b(?:public|private|protected)\s*:|\bvirtual\b|::`)

// cAttributes matches C23 [[...]] attributes and GNU __attribute__((...)).
var cAttributes = regexp.MustCompile(`(?s)\[\[.*?\]\]|\b__attribute__\s*\(\(.*?\)\)`)

// cStructBody matches a struct definition: its name (empty when anonymous)
// and its body up to the first nested brace, the part that declares
// members.
var cStructBody = regexp.MustCompile(`\bstruct\b(?:\s+([A-Za-z_]\w*))?\s*\{([^{}]*)`)

// cppMemberFunction matches a member function in a struct body: a type
// then a name and "(" that does not open a function pointer ("int f()",
// not "int (*f)()"), or a destructor.
var cppMemberFunction = regexp.MustCompile(`\b[A-Za-z_]\w*[\s*&]+[A-Za-z_]\w*\s*\(\s*[^*\s]|~\s*[A-Za-z_]\w*\s*\(`)

// cppCommentsAndLiterals matches C/C++ comments and string and character
// literals, which IsCppHeader ignores.
var cppCommentsAndLiterals = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*|"(?:[^"\\\n]|\\.)*"|'(?:[^'\\\n]|\\.)*'`)

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
