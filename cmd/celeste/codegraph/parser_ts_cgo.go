//go:build cgo

// TypeScript parser backed by tree-sitter. Replaces GenericParser for
// .ts and .tsx files when celeste is built with CGo enabled. The
// regex-based GenericParser produced symbols that matched identifier
// shapes but could not resolve call-graph edges through TypeScript's
// type-aware method dispatch, leaving most TS interfaces with
// edgeCount=0 in the codegraph (documented in SPEC §8.2 and surfaced
// by the Task 19 ⚠ zero-edge warning). An AST-based parser sees the
// real call sites and writes the edges that were previously missing.
//
// Scope for v2.0.0: TypeScript (.ts and .tsx) only. Python and Rust stay
// on the regex GenericParser for now — they aren't the validation
// target for this task and they have no zero-edge warnings in the
// bundled benchmark corpus.
//
// CGo caveat: this file and its dependencies pull in tree-sitter's C
// runtime and the bundled TypeScript/TSX grammars. The //go:build cgo
// constraint at the top gates compilation on CGO_ENABLED=1 — when
// cross-building release binaries from a Linux host for darwin/windows
// the Go toolchain disables CGo implicitly, and the stub in
// parser_ts_stub.go takes over. Stub builds fall back to the regex
// GenericParser for TypeScript files; they still work, just without
// the tree-sitter edge-resolution improvement. Users who want the
// full experience must either build from source with CGo enabled or
// wait for the v2.1.0 release workflow which will cross-compile
// against a proper C toolchain (zig CC or matrix of native runners).
package codegraph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// TSParser parses TypeScript and TSX source files using tree-sitter.
// One parser holds both language pointers — selection is per-file by
// extension. The underlying tree_sitter.Parser is re-used across files
// (Parse() resets the internal state) so allocation stays cheap.
type TSParser struct {
	parser  *tree_sitter.Parser
	tsLang  *tree_sitter.Language
	tsxLang *tree_sitter.Language
}

// NewTSParser initializes the tree-sitter parser with the TypeScript
// and TSX grammars loaded. Returns an error if grammar wiring fails
// (shouldn't happen in practice — the grammars are statically linked).
func NewTSParser() *TSParser {
	return &TSParser{
		parser:  tree_sitter.NewParser(),
		tsLang:  tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()),
		tsxLang: tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX()),
	}
}

// Close releases the tree-sitter parser's native resources.
func (p *TSParser) Close() {
	if p.parser != nil {
		p.parser.Close()
		p.parser = nil
	}
}

// ParseFile reads a .ts or .tsx file and returns extracted symbols +
// edges. Uses the TSX grammar for .tsx files and the plain TypeScript
// grammar for everything else.
func (p *TSParser) ParseFile(path string) (*ParseResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	lang := p.tsLang
	if strings.ToLower(filepath.Ext(path)) == ".tsx" {
		lang = p.tsxLang
	}
	if err := p.parser.SetLanguage(lang); err != nil {
		return nil, fmt.Errorf("set language: %w", err)
	}

	tree := p.parser.Parse(data, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %s: tree-sitter returned nil", path)
	}
	defer tree.Close()

	result := &ParseResult{Source: data}
	w := &tsWalker{
		src:    data,
		path:   path,
		result: result,
	}
	w.walk(tree.RootNode(), "")

	// Deduplicate: tree-sitter gives us one node per declaration, but
	// if the grammar ever emits duplicates (e.g., exported+default
	// class_declaration variants) the symbols_by_kind uniqueness in
	// UpsertSymbol would collapse them anyway. Dedupe here to keep
	// the ParseResult clean for tests and to save an UpsertSymbol
	// round-trip per duplicate.
	result.Symbols = deduplicateSymbols(result.Symbols)
	return result, nil
}

// tsWalker is a single-file cursor over the AST. It accumulates symbols
// and edges into result as it recurses. currentFn tracks the nearest
// enclosing function/method so call_expression nodes can attribute
// edges to the correct caller.
type tsWalker struct {
	src    []byte
	path   string
	result *ParseResult
}

// nodeText returns the source text spanned by a node.
func (w *tsWalker) nodeText(n *tree_sitter.Node) string {
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

// walk recurses over the AST. currentFn is the enclosing function name
// (empty at module scope) used as the source side of any call edges
// discovered in the subtree. Top-level call_expression nodes outside of
// any function are ignored — there's no meaningful caller to attribute
// them to, and they'd just create noise edges against module init code.
func (w *tsWalker) walk(node *tree_sitter.Node, currentFn string) {
	if node == nil {
		return
	}
	kind := node.Kind()

	switch kind {
	case "function_declaration", "generator_function_declaration":
		name := w.nameFromField(node, "name")
		if name != "" {
			w.result.Symbols = append(w.result.Symbols, Symbol{
				Name:      name,
				Kind:      SymbolFunction,
				File:      w.path,
				Line:      int(node.StartPosition().Row) + 1,
				Signature: w.functionSignature(node, name),
			})
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.walk(node.NamedChild(i), name)
		}
		return

	case "method_definition":
		name := w.nameFromField(node, "name")
		if name != "" {
			w.result.Symbols = append(w.result.Symbols, Symbol{
				Name:      name,
				Kind:      SymbolMethod,
				File:      w.path,
				Line:      int(node.StartPosition().Row) + 1,
				Signature: w.functionSignature(node, name),
			})
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.walk(node.NamedChild(i), name)
		}
		return

	case "class_declaration", "abstract_class_declaration":
		name := w.nameFromField(node, "name")
		if name != "" {
			w.result.Symbols = append(w.result.Symbols, Symbol{
				Name: name,
				Kind: SymbolClass,
				File: w.path,
				Line: int(node.StartPosition().Row) + 1,
			})
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.walk(node.NamedChild(i), currentFn)
		}
		return

	case "interface_declaration":
		name := w.nameFromField(node, "name")
		if name != "" {
			w.result.Symbols = append(w.result.Symbols, Symbol{
				Name: name,
				Kind: SymbolInterface,
				File: w.path,
				Line: int(node.StartPosition().Row) + 1,
			})
		}
		// Don't recurse into interface bodies for edges — they hold
		// type signatures, not executable calls.
		return

	case "type_alias_declaration":
		name := w.nameFromField(node, "name")
		if name != "" {
			w.result.Symbols = append(w.result.Symbols, Symbol{
				Name: name,
				Kind: SymbolType,
				File: w.path,
				Line: int(node.StartPosition().Row) + 1,
			})
		}
		return

	case "enum_declaration":
		name := w.nameFromField(node, "name")
		if name != "" {
			w.result.Symbols = append(w.result.Symbols, Symbol{
				Name: name,
				Kind: SymbolType,
				File: w.path,
				Line: int(node.StartPosition().Row) + 1,
			})
		}
		return

	case "lexical_declaration", "variable_declaration":
		// `const foo = () => ...` and `let foo = async () => ...` —
		// extract the binding name as a function symbol when the
		// initializer is an arrow function or function expression.
		// Variable_declarator is a named child of the declaration.
		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child != nil && child.Kind() == "variable_declarator" {
				w.handleVariableDeclarator(child, currentFn)
			}
		}
		return

	case "import_statement":
		// Extract the module source as an import symbol. The source
		// lives in the `source` field as a string_literal; we strip
		// the surrounding quotes.
		src := node.ChildByFieldName("source")
		if src != nil {
			mod := strings.Trim(w.nodeText(src), "'\"`")
			if mod != "" {
				w.result.Symbols = append(w.result.Symbols, Symbol{
					Name: mod,
					Kind: SymbolImport,
					File: w.path,
					Line: int(node.StartPosition().Row) + 1,
				})
			}
		}
		return

	case "call_expression":
		// Attribute the call to the innermost enclosing function. If
		// we're at module scope (currentFn == ""), drop it — there's
		// no caller to point the edge at.
		if currentFn != "" {
			target := w.callTarget(node)
			if target != "" {
				w.result.Edges = append(w.result.Edges, RawEdge{
					SourceName: currentFn,
					TargetName: target,
					Kind:       EdgeCalls,
				})
			}
		}
		// Recurse — argument lists can contain further call_expressions.
		for i := uint(0); i < node.NamedChildCount(); i++ {
			w.walk(node.NamedChild(i), currentFn)
		}
		return
	}

	// Default: recurse into all named children, preserving currentFn.
	for i := uint(0); i < node.NamedChildCount(); i++ {
		w.walk(node.NamedChild(i), currentFn)
	}
}

// nameFromField returns the text of a named child accessed via field.
func (w *tsWalker) nameFromField(node *tree_sitter.Node, field string) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	// property_identifier / identifier nodes contain the raw text.
	return w.nodeText(child)
}

// functionSignature builds a human-readable signature string. Signature
// format mirrors the Go parser's output: `name(params) returnType`.
// Falls back to the raw parameter source text when field access doesn't
// resolve so the shingle generator still gets usable input.
func (w *tsWalker) functionSignature(node *tree_sitter.Node, name string) string {
	var b strings.Builder
	b.WriteString(name)
	params := node.ChildByFieldName("parameters")
	if params != nil {
		b.WriteString(w.nodeText(params))
	} else {
		b.WriteString("()")
	}
	if ret := node.ChildByFieldName("return_type"); ret != nil {
		b.WriteString(" ")
		b.WriteString(w.nodeText(ret))
	}
	return b.String()
}

// handleVariableDeclarator extracts arrow-function bindings as function
// symbols. `const x = () => body` becomes a SymbolFunction named "x".
// If the initializer isn't an arrow/function expression we still
// recurse (so nested calls register) but don't emit a symbol.
func (w *tsWalker) handleVariableDeclarator(decl *tree_sitter.Node, currentFn string) {
	name := w.nameFromField(decl, "name")
	value := decl.ChildByFieldName("value")
	if name != "" && value != nil {
		vkind := value.Kind()
		if vkind == "arrow_function" || vkind == "function_expression" || vkind == "function" {
			w.result.Symbols = append(w.result.Symbols, Symbol{
				Name:      name,
				Kind:      SymbolFunction,
				File:      w.path,
				Line:      int(decl.StartPosition().Row) + 1,
				Signature: w.functionSignature(value, name),
			})
			// Walk the body with `name` as the enclosing function so
			// calls inside the arrow body attribute correctly.
			for i := uint(0); i < value.NamedChildCount(); i++ {
				w.walk(value.NamedChild(i), name)
			}
			return
		}
	}
	// Non-function initializer: recurse with the outer enclosing fn.
	for i := uint(0); i < decl.NamedChildCount(); i++ {
		w.walk(decl.NamedChild(i), currentFn)
	}
}

// callTarget extracts the target name of a call_expression. For simple
// `foo()` calls the target is the identifier text; for member calls
// like `obj.method()` we return `obj.method` (matching the Go parser's
// convention so the downstream edge-resolution unqualified-suffix
// fallback in index.go can still match by the final segment).
func (w *tsWalker) callTarget(call *tree_sitter.Node) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Kind() {
	case "identifier":
		return w.nodeText(fn)
	case "member_expression":
		obj := fn.ChildByFieldName("object")
		prop := fn.ChildByFieldName("property")
		if obj != nil && prop != nil {
			return w.nodeText(obj) + "." + w.nodeText(prop)
		}
		if prop != nil {
			return w.nodeText(prop)
		}
	}
	// Fallthrough: any other expression (computed calls, parenthesized,
	// etc.) isn't a named target we can resolve into an edge.
	return ""
}
