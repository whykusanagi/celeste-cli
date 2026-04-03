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
