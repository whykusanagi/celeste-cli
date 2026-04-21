//go:build cgo

// Multi-language tree-sitter parser. Supports Python, Rust, TypeScript,
// JavaScript, Go, Java, C, C++, C#, Ruby, PHP, Scala, and more via the
// language spec mappings in parser_ts_languages.go.
//
// Each language requires a tree-sitter grammar Go binding. Languages
// without an available Go binding fall back to the regex GenericParser.
package codegraph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// MultiLangParser parses source files across multiple languages using
// tree-sitter grammars and the langSpec node type mappings.
type MultiLangParser struct {
	parser *tree_sitter.Parser
	langs  map[string]*tree_sitter.Language
}

// NewMultiLangParser initializes the parser with all available grammars.
func NewMultiLangParser() *MultiLangParser {
	m := &MultiLangParser{
		parser: tree_sitter.NewParser(),
		langs:  make(map[string]*tree_sitter.Language),
	}

	// Register all available grammars
	m.langs["python"] = tree_sitter.NewLanguage(tree_sitter_python.Language())
	m.langs["rust"] = tree_sitter.NewLanguage(tree_sitter_rust.Language())
	m.langs["typescript"] = tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())
	m.langs["tsx"] = tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX())
	m.langs["javascript"] = tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()) // JS subset of TS grammar
	m.langs["java"] = tree_sitter.NewLanguage(tree_sitter_java.Language())
	m.langs["c"] = tree_sitter.NewLanguage(tree_sitter_c.Language())
	m.langs["cpp"] = tree_sitter.NewLanguage(tree_sitter_cpp.Language())
	m.langs["ruby"] = tree_sitter.NewLanguage(tree_sitter_ruby.Language())
	m.langs["php"] = tree_sitter.NewLanguage(tree_sitter_php.LanguagePHP())

	return m
}

// Close releases native resources.
func (m *MultiLangParser) Close() {
	if m.parser != nil {
		m.parser.Close()
		m.parser = nil
	}
}

// SupportsFile returns true if this parser can handle the given file.
func (m *MultiLangParser) SupportsFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	lang := SupportedLanguage(ext)
	if lang == "" {
		return false
	}
	_, hasGrammar := m.langs[lang]
	_, hasSpec := langSpecs[lang]
	return hasGrammar && hasSpec
}

// ParseFile reads a source file and returns extracted symbols and edges
// using the tree-sitter AST and language-specific node type mappings.
func (m *MultiLangParser) ParseFile(path string) (*ParseResult, error) {
	ext := strings.ToLower(filepath.Ext(path))
	lang := SupportedLanguage(ext)
	if lang == "" {
		return nil, fmt.Errorf("unsupported language for %s", path)
	}

	grammar, ok := m.langs[lang]
	if !ok {
		return nil, fmt.Errorf("no tree-sitter grammar for %s", lang)
	}

	spec, ok := langSpecs[lang]
	if !ok {
		return nil, fmt.Errorf("no lang spec for %s", lang)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	if err := m.parser.SetLanguage(grammar); err != nil {
		return nil, fmt.Errorf("set language %s: %w", lang, err)
	}

	tree := m.parser.Parse(data, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %s: tree-sitter returned nil", path)
	}
	defer tree.Close()

	result := &ParseResult{Source: data}
	w := &multiWalker{
		src:       data,
		path:      path,
		lang:      lang,
		spec:      &spec,
		result:    result,
		classSet:  nodeTypeSet(spec.ClassTypes),
		funcSet:   nodeTypeSet(spec.FunctionTypes),
		importSet: nodeTypeSet(spec.ImportTypes),
		callSet:   nodeTypeSet(spec.CallTypes),
	}
	w.walk(tree.RootNode(), "")

	result.Symbols = deduplicateSymbols(result.Symbols)
	return result, nil
}

// multiWalker traverses a tree-sitter AST using language-specific
// node type mappings to extract symbols and edges.
type multiWalker struct {
	src       []byte
	path      string
	lang      string
	spec      *langSpec
	result    *ParseResult
	classSet  map[string]bool
	funcSet   map[string]bool
	importSet map[string]bool
	callSet   map[string]bool
}

func (w *multiWalker) nodeText(n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	start := n.StartByte()
	end := n.EndByte()
	if end > uint(len(w.src)) {
		end = uint(len(w.src))
	}
	if start >= end {
		return ""
	}
	return string(w.src[start:end])
}

func (w *multiWalker) walk(node *tree_sitter.Node, currentFn string) {
	if node == nil {
		return
	}
	kind := node.Kind()

	// Class/struct/enum/trait declarations
	if w.classSet[kind] {
		name := w.extractName(node)
		if name != "" {
			symKind := w.classifyClassKind(kind)
			w.result.Symbols = append(w.result.Symbols, Symbol{
				Name: name,
				Kind: symKind,
				File: w.path,
				Line: int(node.StartPosition().Row) + 1,
			})
		}
		// Recurse into class body for methods
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.walk(node.NamedChild(i), currentFn)
		}
		return
	}

	// Function/method declarations
	if w.funcSet[kind] {
		name := w.extractName(node)
		if name != "" {
			symKind := SymbolFunction
			// Heuristic: if we're inside a class context or the node
			// type contains "method", treat as method
			if strings.Contains(kind, "method") || currentFn != "" && w.isMethodContext(node) {
				symKind = SymbolMethod
			}
			sig := w.buildSignature(node, name)
			w.result.Symbols = append(w.result.Symbols, Symbol{
				Name:      name,
				Kind:      symKind,
				File:      w.path,
				Line:      int(node.StartPosition().Row) + 1,
				Signature: sig,
			})
		}
		fnName := name
		if fnName == "" {
			fnName = currentFn
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.walk(node.NamedChild(i), fnName)
		}
		return
	}

	// Import statements
	if w.importSet[kind] {
		name := w.extractImportName(node)
		if name != "" {
			w.result.Symbols = append(w.result.Symbols, Symbol{
				Name: name,
				Kind: SymbolImport,
				File: w.path,
				Line: int(node.StartPosition().Row) + 1,
			})
		}
		return
	}

	// Call expressions → edges
	if w.callSet[kind] && currentFn != "" {
		target := w.extractCallTarget(node)
		if target != "" {
			w.result.Edges = append(w.result.Edges, RawEdge{
				SourceName: currentFn,
				TargetName: target,
				Kind:       EdgeCalls,
			})
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.walk(node.NamedChild(i), currentFn)
		}
		return
	}

	// Variable declarations that bind arrow functions (JS/TS pattern)
	if (kind == "lexical_declaration" || kind == "variable_declaration") &&
		(w.lang == "javascript" || w.lang == "typescript" || w.lang == "tsx") {
		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child != nil && child.Kind() == "variable_declarator" {
				w.handleVarDeclarator(child, currentFn)
			}
		}
		return
	}

	// Default: recurse
	for i := uint(0); i < node.NamedChildCount(); i++ {
		w.walk(node.NamedChild(i), currentFn)
	}
}

// extractName gets the identifier name from a declaration node.
func (w *multiWalker) extractName(node *tree_sitter.Node) string {
	// Try the configured name field first
	if w.spec.NameField != "" {
		if child := node.ChildByFieldName(w.spec.NameField); child != nil {
			text := w.nodeText(child)
			// For C/C++ declarators, strip pointer/ref markers and parens
			if w.spec.NameField == "declarator" {
				text = extractCIdentifier(text)
			}
			return text
		}
	}
	// Fallback: try "name" field
	if child := node.ChildByFieldName("name"); child != nil {
		return w.nodeText(child)
	}
	// Last resort: first identifier child
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "identifier" || child.Kind() == "type_identifier" {
			return w.nodeText(child)
		}
	}
	return ""
}

// extractImportName gets the module/package name from an import node.
func (w *multiWalker) extractImportName(node *tree_sitter.Node) string {
	// Python: import_from_statement has "module_name" field
	if child := node.ChildByFieldName("module_name"); child != nil {
		return w.nodeText(child)
	}
	// JS/TS: source field contains the string literal
	if child := node.ChildByFieldName("source"); child != nil {
		return strings.Trim(w.nodeText(child), "'\"`")
	}
	// Go: import_declaration contains import_spec with path
	if child := node.ChildByFieldName("path"); child != nil {
		return strings.Trim(w.nodeText(child), "\"")
	}
	// Rust: use_declaration — get the whole path
	if w.lang == "rust" {
		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			k := child.Kind()
			if k == "scoped_identifier" || k == "identifier" || k == "use_wildcard" || k == "scoped_use_list" {
				return w.nodeText(child)
			}
		}
	}
	// Java/C#/Scala: identifier or scoped_identifier
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		k := child.Kind()
		if k == "scoped_identifier" || k == "identifier" || k == "qualified_name" {
			return w.nodeText(child)
		}
		// C/C++ preproc_include: string_literal or system_lib_string
		if k == "string_literal" || k == "system_lib_string" {
			return strings.Trim(w.nodeText(child), "\"<>")
		}
	}
	// Fallback: first named child text
	if node.NamedChildCount() > 0 {
		return w.nodeText(node.NamedChild(0))
	}
	return ""
}

// extractCallTarget gets the function/method name from a call node.
func (w *multiWalker) extractCallTarget(node *tree_sitter.Node) string {
	// Try "function" field (JS/TS/Go/Rust/Java/C/C++)
	if fn := node.ChildByFieldName("function"); fn != nil {
		return w.identFromExpr(fn)
	}
	// Try "method" field (Java method_invocation)
	if method := node.ChildByFieldName("method"); method != nil {
		return w.nodeText(method)
	}
	// Try "name" field (Ruby, PHP)
	if name := node.ChildByFieldName("name"); name != nil {
		return w.nodeText(name)
	}
	// Python: call node has first child as the callee
	if w.lang == "python" && node.NamedChildCount() > 0 {
		return w.identFromExpr(node.NamedChild(0))
	}
	return ""
}

// identFromExpr extracts an identifier from an expression node.
func (w *multiWalker) identFromExpr(node *tree_sitter.Node) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier", "type_identifier":
		return w.nodeText(node)
	case "member_expression", "field_expression", "attribute":
		// obj.method → "obj.method"
		obj := node.ChildByFieldName("object")
		prop := node.ChildByFieldName("property")
		if prop == nil {
			prop = node.ChildByFieldName("attribute")
		}
		if prop == nil {
			prop = node.ChildByFieldName("field")
		}
		if obj != nil && prop != nil {
			return w.nodeText(obj) + "." + w.nodeText(prop)
		}
		if prop != nil {
			return w.nodeText(prop)
		}
	case "scoped_identifier":
		return w.nodeText(node)
	}
	return ""
}

// classifyClassKind maps a tree-sitter node type to the appropriate SymbolKind.
func (w *multiWalker) classifyClassKind(nodeType string) SymbolKind {
	switch {
	case strings.Contains(nodeType, "interface") || strings.Contains(nodeType, "protocol") || strings.Contains(nodeType, "trait"):
		return SymbolInterface
	case strings.Contains(nodeType, "struct"):
		return SymbolStruct
	case strings.Contains(nodeType, "enum"):
		return SymbolType
	case strings.Contains(nodeType, "type_declaration") || strings.Contains(nodeType, "type_definition"):
		return SymbolType
	default:
		return SymbolClass
	}
}

// isMethodContext checks if a function node is inside a class body.
func (w *multiWalker) isMethodContext(node *tree_sitter.Node) bool {
	parent := node.Parent()
	for parent != nil {
		pk := parent.Kind()
		if w.classSet[pk] {
			return true
		}
		// Stop at module/program level
		if pk == "program" || pk == "source_file" || pk == "module" {
			return false
		}
		parent = parent.Parent()
	}
	return false
}

// buildSignature constructs a human-readable signature from a function node.
func (w *multiWalker) buildSignature(node *tree_sitter.Node, name string) string {
	var b strings.Builder
	b.WriteString(name)

	// Try "parameters" field
	if params := node.ChildByFieldName("parameters"); params != nil {
		b.WriteString(w.nodeText(params))
	} else {
		b.WriteString("()")
	}

	// Try return type
	if ret := node.ChildByFieldName("return_type"); ret != nil {
		b.WriteString(" ")
		b.WriteString(w.nodeText(ret))
	} else if ret := node.ChildByFieldName("type"); ret != nil {
		b.WriteString(" ")
		b.WriteString(w.nodeText(ret))
	}

	return b.String()
}

// handleVarDeclarator handles `const x = () => ...` patterns in JS/TS.
func (w *multiWalker) handleVarDeclarator(decl *tree_sitter.Node, currentFn string) {
	nameNode := decl.ChildByFieldName("name")
	value := decl.ChildByFieldName("value")
	if nameNode == nil || value == nil {
		return
	}
	name := w.nodeText(nameNode)
	vkind := value.Kind()
	if vkind == "arrow_function" || vkind == "function_expression" || vkind == "function" {
		w.result.Symbols = append(w.result.Symbols, Symbol{
			Name:      name,
			Kind:      SymbolFunction,
			File:      w.path,
			Line:      int(decl.StartPosition().Row) + 1,
			Signature: w.buildSignature(value, name),
		})
		for i := uint(0); i < value.NamedChildCount(); i++ {
			w.walk(value.NamedChild(i), name)
		}
		return
	}
	for i := uint(0); i < decl.NamedChildCount(); i++ {
		w.walk(decl.NamedChild(i), currentFn)
	}
}

// multiLangGrammars is the set of languages that have tree-sitter Go
// bindings available. Checked by tryMultiParser without allocating a parser.
var multiLangGrammars = map[string]bool{
	"python": true, "rust": true, "typescript": true, "tsx": true,
	"javascript": true, "java": true, "c": true, "cpp": true,
	"ruby": true, "php": true,
}

// tryMultiParser returns true if the multi-language parser supports this file.
func (idx *Indexer) tryMultiParser(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	lang := SupportedLanguage(ext)
	if lang == "" || lang == "go" {
		return false // Go has its own AST parser
	}
	return multiLangGrammars[lang] && langSpecs[lang].FunctionTypes != nil
}

// extractCIdentifier strips pointer/ref markers and parens from a C/C++
// declarator to get the bare function name.
func extractCIdentifier(s string) string {
	s = strings.TrimLeft(s, "*&")
	if idx := strings.IndexByte(s, '('); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
