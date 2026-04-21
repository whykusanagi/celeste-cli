//go:build !cgo

// Stub for non-CGo builds — language spec lookups still work for
// extension detection and fallback to the generic regex parser.
// The actual tree-sitter parsing requires CGo.
package codegraph

// langSpec defines the tree-sitter node types for a single language.
type langSpec struct {
	ClassTypes    []string
	FunctionTypes []string
	ImportTypes   []string
	CallTypes     []string
	NameField     string
	TestPatterns  []string
}

// extToLang maps file extensions to language identifiers.
var extToLang = map[string]string{
	".py":    "python",
	".pyi":   "python",
	".js":    "javascript",
	".jsx":   "javascript",
	".mjs":   "javascript",
	".ts":    "typescript",
	".tsx":   "tsx",
	".go":    "go",
	".rs":    "rust",
	".java":  "java",
	".c":     "c",
	".h":     "c",
	".cc":    "cpp",
	".cpp":   "cpp",
	".cxx":   "cpp",
	".hpp":   "cpp",
	".cs":    "csharp",
	".rb":    "ruby",
	".kt":    "kotlin",
	".swift": "swift",
	".php":   "php",
	".scala": "scala",
	".dart":  "dart",
	".lua":   "lua",
	".zig":   "zig",
	".sh":    "bash",
	".bash":  "bash",
	".zsh":   "bash",
	".jl":    "julia",
}

// LookupLangSpec returns nil in non-CGo builds (no tree-sitter available).
func LookupLangSpec(ext string) *langSpec { return nil }

// SupportedLanguage returns the language name for a file extension.
func SupportedLanguage(ext string) string { return extToLang[ext] }

// nodeTypeSet converts a string slice to a map for O(1) membership checks.
func nodeTypeSet(types []string) map[string]bool {
	m := make(map[string]bool, len(types))
	for _, t := range types {
		m[t] = true
	}
	return m
}
