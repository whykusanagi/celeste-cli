//go:build cgo

// Multi-language tree-sitter AST node type mappings.
//
// Defines which tree-sitter node types correspond to classes, functions,
// imports, and calls for each supported language. Used by the tree-sitter
// parser to extract symbols and edges from source files.
//
// Reference: derived from code-review-graph's parser.py (MIT licensed,
// github.com/tirth8205/code-review-graph) and adapted for celeste-cli's
// codegraph symbol model.
package codegraph

// langSpec defines the tree-sitter node types for a single language.
type langSpec struct {
	// ClassTypes are AST node types that define classes, structs, enums,
	// interfaces, traits, or other type-level declarations.
	ClassTypes []string

	// FunctionTypes are AST node types that define functions, methods,
	// constructors, or other callable declarations.
	FunctionTypes []string

	// ImportTypes are AST node types for import/include/use statements.
	ImportTypes []string

	// CallTypes are AST node types for function/method invocations.
	CallTypes []string

	// NameField is the field name used to extract the identifier from
	// declarations (usually "name", but some grammars differ).
	NameField string

	// TestPatterns are function name prefixes/suffixes that indicate tests.
	TestPatterns []string
}

// langSpecs maps language identifiers to their tree-sitter node type
// mappings. Extension-to-language mapping is in extToLang below.
var langSpecs = map[string]langSpec{
	"python": {
		ClassTypes:    []string{"class_definition"},
		FunctionTypes: []string{"function_definition"},
		ImportTypes:   []string{"import_statement", "import_from_statement"},
		CallTypes:     []string{"call"},
		NameField:     "name",
		TestPatterns:  []string{"test_", "Test"},
	},
	"rust": {
		ClassTypes:    []string{"struct_item", "enum_item", "impl_item", "trait_item"},
		FunctionTypes: []string{"function_item"},
		ImportTypes:   []string{"use_declaration"},
		CallTypes:     []string{"call_expression", "macro_invocation"},
		NameField:     "name",
		TestPatterns:  []string{"test_"},
	},
	"typescript": {
		ClassTypes:    []string{"class_declaration", "class", "abstract_class_declaration"},
		FunctionTypes: []string{"function_declaration", "method_definition", "arrow_function"},
		ImportTypes:   []string{"import_statement"},
		CallTypes:     []string{"call_expression", "new_expression"},
		NameField:     "name",
		TestPatterns:  []string{"test", "it(", "describe("},
	},
	"tsx": {
		ClassTypes:    []string{"class_declaration", "class"},
		FunctionTypes: []string{"function_declaration", "method_definition", "arrow_function"},
		ImportTypes:   []string{"import_statement"},
		CallTypes:     []string{"call_expression", "new_expression"},
		NameField:     "name",
		TestPatterns:  []string{"test", "it(", "describe("},
	},
	"javascript": {
		ClassTypes:    []string{"class_declaration", "class"},
		FunctionTypes: []string{"function_declaration", "method_definition", "arrow_function"},
		ImportTypes:   []string{"import_statement"},
		CallTypes:     []string{"call_expression", "new_expression"},
		NameField:     "name",
		TestPatterns:  []string{"test", "it(", "describe("},
	},
	"go": {
		ClassTypes:    []string{"type_declaration"},
		FunctionTypes: []string{"function_declaration", "method_declaration"},
		ImportTypes:   []string{"import_declaration"},
		CallTypes:     []string{"call_expression"},
		NameField:     "name",
		TestPatterns:  []string{"Test", "Benchmark"},
	},
	"java": {
		ClassTypes:    []string{"class_declaration", "interface_declaration", "enum_declaration"},
		FunctionTypes: []string{"method_declaration", "constructor_declaration"},
		ImportTypes:   []string{"import_declaration"},
		CallTypes:     []string{"method_invocation", "object_creation_expression"},
		NameField:     "name",
		TestPatterns:  []string{"test", "Test"},
	},
	"c": {
		ClassTypes:    []string{"struct_specifier", "type_definition", "enum_specifier"},
		FunctionTypes: []string{"function_definition"},
		ImportTypes:   []string{"preproc_include"},
		CallTypes:     []string{"call_expression"},
		NameField:     "declarator",
		TestPatterns:  []string{"test_", "Test"},
	},
	"cpp": {
		ClassTypes:    []string{"class_specifier", "struct_specifier"},
		FunctionTypes: []string{"function_definition"},
		ImportTypes:   []string{"preproc_include"},
		CallTypes:     []string{"call_expression"},
		NameField:     "declarator",
		TestPatterns:  []string{"test_", "Test", "TEST"},
	},
	"csharp": {
		ClassTypes:    []string{"class_declaration", "interface_declaration", "enum_declaration", "struct_declaration"},
		FunctionTypes: []string{"method_declaration", "constructor_declaration"},
		ImportTypes:   []string{"using_directive"},
		CallTypes:     []string{"invocation_expression", "object_creation_expression"},
		NameField:     "name",
		TestPatterns:  []string{"Test", "test"},
	},
	"ruby": {
		ClassTypes:    []string{"class", "module"},
		FunctionTypes: []string{"method", "singleton_method"},
		ImportTypes:   []string{"call"}, // require/require_relative
		CallTypes:     []string{"call", "method_call"},
		NameField:     "name",
		TestPatterns:  []string{"test_"},
	},
	"kotlin": {
		ClassTypes:    []string{"class_declaration", "object_declaration"},
		FunctionTypes: []string{"function_declaration"},
		ImportTypes:   []string{"import_header"},
		CallTypes:     []string{"call_expression"},
		NameField:     "name",
		TestPatterns:  []string{"test", "Test"},
	},
	"swift": {
		ClassTypes:    []string{"class_declaration", "struct_declaration", "protocol_declaration"},
		FunctionTypes: []string{"function_declaration"},
		ImportTypes:   []string{"import_declaration"},
		CallTypes:     []string{"call_expression"},
		NameField:     "name",
		TestPatterns:  []string{"test", "Test"},
	},
	"php": {
		ClassTypes:    []string{"class_declaration", "interface_declaration"},
		FunctionTypes: []string{"function_definition", "method_declaration"},
		ImportTypes:   []string{"namespace_use_declaration"},
		CallTypes:     []string{"function_call_expression", "member_call_expression"},
		NameField:     "name",
		TestPatterns:  []string{"test", "Test"},
	},
	"scala": {
		ClassTypes:    []string{"class_definition", "trait_definition", "object_definition", "enum_definition"},
		FunctionTypes: []string{"function_definition", "function_declaration"},
		ImportTypes:   []string{"import_declaration"},
		CallTypes:     []string{"call_expression", "instance_expression", "generic_function"},
		NameField:     "name",
		TestPatterns:  []string{"test", "Test"},
	},
	"dart": {
		ClassTypes:    []string{"class_definition", "mixin_declaration", "enum_declaration"},
		FunctionTypes: []string{"function_signature"},
		ImportTypes:   []string{"import_or_export"},
		CallTypes:     []string{"call_expression"}, // not in reference but standard
		NameField:     "name",
		TestPatterns:  []string{"test", "Test"},
	},
	"lua": {
		ClassTypes:    nil, // table-based OOP, no class keyword
		FunctionTypes: []string{"function_declaration"},
		ImportTypes:   nil, // require() is a call, handled by call extraction
		CallTypes:     []string{"function_call"},
		NameField:     "name",
		TestPatterns:  []string{"test_"},
	},
	"zig": {
		ClassTypes:    []string{"container_declaration"},
		FunctionTypes: []string{"fn_proto", "fn_decl"},
		ImportTypes:   nil, // @import is a builtin_call_expr
		CallTypes:     []string{"call_expression", "builtin_call_expr"},
		NameField:     "name",
		TestPatterns:  []string{"test"},
	},
	"bash": {
		ClassTypes:    nil,
		FunctionTypes: []string{"function_definition"},
		ImportTypes:   nil, // source/. handled separately
		CallTypes:     []string{"command"},
		NameField:     "name",
		TestPatterns:  nil,
	},
	"julia": {
		ClassTypes:    []string{"struct_definition", "abstract_definition"},
		FunctionTypes: []string{"function_definition", "short_function_definition"},
		ImportTypes:   []string{"import_statement", "using_statement"},
		CallTypes:     []string{"call_expression"},
		NameField:     "name",
		TestPatterns:  []string{"test_", "Test"},
	},
}

// extToLang maps file extensions to language identifiers for tree-sitter
// grammar selection. Extensions must include the leading dot.
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

// LookupLangSpec returns the language spec for a file extension.
// Returns nil if the language is not supported.
func LookupLangSpec(ext string) *langSpec {
	lang, ok := extToLang[ext]
	if !ok {
		return nil
	}
	spec, ok := langSpecs[lang]
	if !ok {
		return nil
	}
	return &spec
}

// SupportedLanguage returns the language name for a file extension,
// or empty string if unsupported.
func SupportedLanguage(ext string) string {
	return extToLang[ext]
}

// nodeTypeSet converts a string slice to a map for O(1) membership checks.
func nodeTypeSet(types []string) map[string]bool {
	m := make(map[string]bool, len(types))
	for _, t := range types {
		m[t] = true
	}
	return m
}
