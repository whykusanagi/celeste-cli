package codegraph

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// GenericParser extracts symbols from non-Go source files using regex patterns.
// Covers Python, JavaScript, TypeScript, and Rust. No call graph (would need
// tree-sitter / CGo). Focuses on declarations: functions, classes, imports.
type GenericParser struct {
	language string
	patterns languagePatterns
}

type languagePatterns struct {
	function  []*regexp.Regexp
	class     []*regexp.Regexp
	iface     []*regexp.Regexp
	typeDecl  []*regexp.Regexp
	structDcl []*regexp.Regexp
	importDcl []*regexp.Regexp
	constDecl []*regexp.Regexp
}

// NewGenericParser creates a parser for the given language.
func NewGenericParser(language string) *GenericParser {
	p := &GenericParser{language: language}
	p.patterns = p.patternsForLanguage(language)
	return p
}

// ParseFile parses a source file and extracts symbols using regex.
func (p *GenericParser) ParseFile(path string) (*ParseResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	source := string(data)
	lines := strings.Split(source, "\n")
	result := &ParseResult{Source: data}

	// Track whether each line is inside a class for method detection
	var currentClass string
	classIndent := -1

	for lineNum, line := range lines {
		lineNo := lineNum + 1 // 1-based

		// Check class declarations (before function to set context)
		for _, re := range p.patterns.class {
			if m := re.FindStringSubmatch(line); m != nil {
				name := m[1]
				result.Symbols = append(result.Symbols, Symbol{
					Name: name, Kind: SymbolClass, File: path, Line: lineNo,
				})
				currentClass = name
				classIndent = countLeadingSpaces(line)
			}
		}

		// Detect if we've left the class scope (for Python indentation)
		if currentClass != "" && p.language == "python" {
			indent := countLeadingSpaces(line)
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && indent <= classIndent && !strings.HasPrefix(trimmed, "class ") && !strings.HasPrefix(trimmed, "#") {
				currentClass = ""
				classIndent = -1
			}
		}

		// Check function/method declarations
		for _, re := range p.patterns.function {
			if m := re.FindStringSubmatch(line); m != nil {
				name := m[1]
				// In Python, methods are indented functions inside a class
				if p.language == "python" && currentClass != "" {
					indent := countLeadingSpaces(line)
					if indent > classIndent {
						result.Symbols = append(result.Symbols, Symbol{
							Name: name, Kind: SymbolMethod, File: path, Line: lineNo,
						})
						continue
					}
				}
				result.Symbols = append(result.Symbols, Symbol{
					Name: name, Kind: SymbolFunction, File: path, Line: lineNo,
				})
			}
		}

		// Check interface declarations
		for _, re := range p.patterns.iface {
			if m := re.FindStringSubmatch(line); m != nil {
				result.Symbols = append(result.Symbols, Symbol{
					Name: m[1], Kind: SymbolInterface, File: path, Line: lineNo,
				})
			}
		}

		// Check type declarations
		for _, re := range p.patterns.typeDecl {
			if m := re.FindStringSubmatch(line); m != nil {
				result.Symbols = append(result.Symbols, Symbol{
					Name: m[1], Kind: SymbolType, File: path, Line: lineNo,
				})
			}
		}

		// Check struct declarations
		for _, re := range p.patterns.structDcl {
			if m := re.FindStringSubmatch(line); m != nil {
				result.Symbols = append(result.Symbols, Symbol{
					Name: m[1], Kind: SymbolStruct, File: path, Line: lineNo,
				})
			}
		}

		// Check import declarations
		for _, re := range p.patterns.importDcl {
			if m := re.FindStringSubmatch(line); m != nil {
				importName := m[1]
				result.Symbols = append(result.Symbols, Symbol{
					Name: importName, Kind: SymbolImport, File: path, Line: lineNo,
				})
			}
		}

		// Check const declarations
		for _, re := range p.patterns.constDecl {
			if m := re.FindStringSubmatch(line); m != nil {
				result.Symbols = append(result.Symbols, Symbol{
					Name: m[1], Kind: SymbolConst, File: path, Line: lineNo,
				})
			}
		}
	}

	// Deduplicate symbols (method patterns may overlap with function patterns)
	result.Symbols = deduplicateSymbols(result.Symbols)

	// Extract call edges from function/method bodies
	result.Edges = p.extractCallEdges(source, result.Symbols)

	return result, nil
}

func (p *GenericParser) patternsForLanguage(lang string) languagePatterns {
	switch lang {
	case "python":
		return languagePatterns{
			function:  compileAll(`^\s*def\s+(\w+)\s*\(`),
			class:     compileAll(`^\s*class\s+(\w+)`),
			importDcl: compileAll(`^import\s+(\w+)`, `^from\s+(\S+)\s+import`),
		}
	case "javascript":
		return languagePatterns{
			function: compileAll(
				`^\s*function\s+(\w+)\s*\(`,
				`^\s*(?:const|let|var)\s+(\w+)\s*=\s*(?:\([^)]*\)|[^=])\s*=>`,
			),
			class:     compileAll(`^\s*(?:export\s+)?(?:default\s+)?class\s+(\w+)`),
			importDcl: compileAll(`^\s*import\s+.*from\s+['"]([^'"]+)['"]`, `^\s*(?:const|let|var)\s+\w+\s*=\s*require\s*\(\s*['"]([^'"]+)['"]\s*\)`),
		}
	case "typescript":
		return languagePatterns{
			function: compileAll(
				`^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)`,
				`^\s*(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\([^)]*\)\s*(?::\s*\S+)?\s*=>`,
			),
			class:     compileAll(`^\s*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+(\w+)`),
			iface:     compileAll(`^\s*(?:export\s+)?interface\s+(\w+)`),
			typeDecl:  compileAll(`^\s*(?:export\s+)?type\s+(\w+)\s*=`),
			importDcl: compileAll(`^\s*import\s+.*from\s+['"]([^'"]+)['"]`),
		}
	case "rust":
		return languagePatterns{
			function:  compileAll(`^\s*(?:pub\s+)?(?:async\s+)?fn\s+(\w+)`),
			structDcl: compileAll(`^\s*(?:pub\s+)?struct\s+(\w+)`),
			iface:     compileAll(`^\s*(?:pub\s+)?trait\s+(\w+)`),
			importDcl: compileAll(`^\s*use\s+([^;{]+)`),
			constDecl: compileAll(`^\s*(?:pub\s+)?const\s+(\w+)\s*:`),
		}
	default:
		// Fallback: try common patterns
		return languagePatterns{
			function: compileAll(`^\s*(?:function|def|fn|func)\s+(\w+)`),
			class:    compileAll(`^\s*class\s+(\w+)`),
		}
	}
}

func compileAll(patterns ...string) []*regexp.Regexp {
	result := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		result[i] = regexp.MustCompile(p)
	}
	return result
}

func countLeadingSpaces(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 4
		} else {
			break
		}
	}
	return count
}

// callPattern matches identifiers followed by '(' — a simple call heuristic.
var callPattern = regexp.MustCompile(`\b([a-zA-Z_]\w*)\s*\(`)

// extractCallEdges scans each function/method body for call-like patterns and
// creates edges to known symbols. Works for JS/TS/Python/Rust and any language
// where calls look like `name(`.
func (p *GenericParser) extractCallEdges(source string, symbols []Symbol) []RawEdge {
	var edges []RawEdge

	// Build a set of known callable symbol names
	knownSymbols := make(map[string]bool)
	for _, s := range symbols {
		if s.Kind == SymbolFunction || s.Kind == SymbolMethod {
			knownSymbols[s.Name] = true
		}
	}

	for _, sym := range symbols {
		if sym.Kind != SymbolFunction && sym.Kind != SymbolMethod {
			continue
		}
		body := extractBody(source, sym.Line)
		matches := callPattern.FindAllStringSubmatch(body, -1)
		seen := make(map[string]bool)
		for _, match := range matches {
			callee := match[1]
			if isGenericKeyword(callee) || callee == sym.Name || seen[callee] {
				continue
			}
			if knownSymbols[callee] {
				seen[callee] = true
				edges = append(edges, RawEdge{
					SourceName: sym.Name,
					TargetName: callee,
					Kind:       EdgeCalls,
				})
			}
		}
	}
	return edges
}

// extractBody returns up to 50 lines starting from startLine (1-based).
func extractBody(source string, startLine int) string {
	lines := strings.Split(source, "\n")
	if startLine <= 0 || startLine > len(lines) {
		return ""
	}
	end := startLine + 50
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[startLine-1:end], "\n")
}

// isGenericKeyword returns true for common keywords across JS/TS/Python/Rust
// that should not be treated as function calls.
var genericKeywords = map[string]bool{
	// JS/TS
	"if": true, "else": true, "for": true, "while": true, "return": true,
	"function": true, "class": true, "const": true, "let": true, "var": true,
	"new": true, "this": true, "super": true, "import": true, "export": true,
	"async": true, "await": true, "try": true, "catch": true, "throw": true,
	"typeof": true, "instanceof": true, "switch": true, "case": true,
	"console": true, "require": true, "module": true, "true": true, "false": true,
	"null": true, "undefined": true, "void": true, "delete": true,
	// Python
	"def": true, "elif": true, "except": true, "finally": true, "from": true,
	"global": true, "lambda": true, "nonlocal": true, "not": true, "or": true,
	"and": true, "pass": true, "raise": true, "with": true, "yield": true,
	"print": true, "self": true, "None": true, "True": true, "False": true,
	// Rust
	"fn": true, "pub": true, "impl": true, "trait": true, "struct": true,
	"enum": true, "mod": true, "use": true, "crate": true, "match": true,
	"where": true, "loop": true, "break": true, "continue": true, "move": true,
	"mut": true, "ref": true, "unsafe": true, "type": true, "as": true,
	"in": true, "dyn": true,
}

func isGenericKeyword(s string) bool {
	return genericKeywords[s]
}

// deduplicateSymbols removes duplicate symbols (same name+kind+file+line).
func deduplicateSymbols(syms []Symbol) []Symbol {
	seen := make(map[string]bool)
	var result []Symbol
	for _, s := range syms {
		key := fmt.Sprintf("%s:%s:%s:%d", s.Name, s.Kind, s.File, s.Line)
		if !seen[key] {
			seen[key] = true
			result = append(result, s)
		}
	}
	return result
}
