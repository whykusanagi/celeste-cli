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
	classLine := 0
	// enterContainer records a class-like declaration whose indented
	// functions are methods. PHP also counts interfaces, traits and enums.
	enterContainer := func(name, line string, lineNo int) {
		currentClass = name
		classIndent = countLeadingSpaces(line)
		classLine = lineNo
	}

	for lineNum, line := range lines {
		lineNo := lineNum + 1 // 1-based

		// Check class declarations (before function to set context)
		for _, re := range p.patterns.class {
			if m := re.FindStringSubmatch(line); m != nil {
				name := m[1]
				result.Symbols = append(result.Symbols, Symbol{
					Name: name, Kind: SymbolClass, File: path, Line: lineNo,
				})
				enterContainer(name, line, lineNo)
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
		// PHP: the container ends at the first line back at its own indent
		// (normally its closing brace). An Allman-style opening brace on
		// its own line does not end it.
		if currentClass != "" && p.language == "php" && lineNo != classLine {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && trimmed != "{" && countLeadingSpaces(line) <= classIndent {
				currentClass = ""
				classIndent = -1
			}
		}

		// Check function/method declarations
		for _, re := range p.patterns.function {
			if m := re.FindStringSubmatch(line); m != nil {
				name := m[1]
				// In Python, methods are indented functions inside a class
				if (p.language == "python" || p.language == "php") && currentClass != "" {
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
				if p.language == "php" {
					enterContainer(m[1], line, lineNo)
				}
			}
		}

		// Check type declarations
		for _, re := range p.patterns.typeDecl {
			if m := re.FindStringSubmatch(line); m != nil {
				result.Symbols = append(result.Symbols, Symbol{
					Name: m[1], Kind: SymbolType, File: path, Line: lineNo,
				})
				if p.language == "php" {
					enterContainer(m[1], line, lineNo)
				}
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
	case "php":
		// Kinds follow the tree-sitter path: a trait is an interface, an
		// enum is a type. Only unindented `use` lines are imports; an
		// indented `use Foo;` inside a class body is a trait use.
		return languagePatterns{
			function:  compileAll(`^\s*(?:(?:public|protected|private|static|final|abstract)\s+)*function\s+&?(\w+)\s*\(`),
			class:     compileAll(`^\s*(?:(?:final|abstract|readonly)\s+)*class\s+(\w+)`),
			iface:     compileAll(`^\s*interface\s+(\w+)`, `^\s*trait\s+(\w+)`),
			typeDecl:  compileAll(`^\s*enum\s+(\w+)`),
			importDcl: compileAll(`^use\s+(?:function\s+|const\s+)?\\?([\w\\]+)`),
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

// declPrefix matches the text just before a `name(` that declares or
// constructs rather than calls: `def name(`, `function &name(`, `new Name(`.
var declPrefix = regexp.MustCompile(`\b(?:def|function|fn|func|class|new)\s+&?$`)

// extractCallEdges scans each function/method body for call-like patterns and
// creates an edge for every called name. Works for JS/TS/Python/Rust/PHP and
// any language where calls look like `name(`. Targets need not be declared in
// this file: the indexer resolves names once every file's symbols are stored
// and drops the ones nothing declares, so a cross-file call keeps its edge.
func (p *GenericParser) extractCallEdges(source string, symbols []Symbol) []RawEdge {
	var edges []RawEdge

	for _, sym := range symbols {
		if sym.Kind != SymbolFunction && sym.Kind != SymbolMethod {
			continue
		}
		body := p.functionBody(source, sym.Line)
		matches := callPattern.FindAllStringSubmatchIndex(body, -1)
		seen := make(map[string]bool)
		for _, m := range matches {
			callee := body[m[2]:m[3]]
			if p.isKeyword(callee) || callee == sym.Name || seen[callee] {
				continue
			}
			if declPrefix.MatchString(body[max(0, m[2]-16):m[2]]) {
				continue
			}
			seen[callee] = true
			edges = append(edges, RawEdge{
				SourceName: sym.Name,
				TargetName: callee,
				Kind:       EdgeCalls,
			})
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

// functionBody returns the source of the function declared at startLine,
// trimmed to the function itself so its call scan cannot pick up the calls
// of the functions declared after it. Languages without a known body rule
// keep the 50-line window.
func (p *GenericParser) functionBody(source string, startLine int) string {
	body := extractBody(source, startLine)
	switch p.language {
	case "python":
		return extractIndentedBody(body)
	case "javascript", "typescript", "rust", "php":
		return extractBracedBody(body, p.language)
	}
	return body
}

// extractIndentedBody trims a Python body window to the def itself: the
// signature (which may span lines until its brackets close) plus every
// following line up to the first non-blank, non-comment line indented no
// deeper than the def.
func extractIndentedBody(body string) string {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 {
		return body
	}
	defIndent := countLeadingSpaces(lines[0])
	depth := 0
	i := 0
	// The signature: the def line and any continuation lines.
	for ; i < len(lines); i++ {
		depth += bracketDelta(lines[i])
		if depth <= 0 {
			i++
			break
		}
	}
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if countLeadingSpaces(lines[i]) <= defIndent {
			break
		}
	}
	return strings.Join(lines[:i], "\n")
}

// bracketDelta returns opening minus closing brackets on a line, ignoring
// anything after a '#' comment. Quoted brackets are rare enough in a
// signature to ignore.
func bracketDelta(line string) int {
	if c := strings.IndexByte(line, '#'); c >= 0 {
		line = line[:c]
	}
	d := 0
	for _, ch := range line {
		switch ch {
		case '(', '[', '{':
			d++
		case ')', ']', '}':
			d--
		}
	}
	return d
}

// extractBracedBody trims a brace-language body window to the declaration
// itself: everything up to the brace that closes the body, or up to the
// first top-level ';' when the declaration has no body (an abstract or
// interface method, a one-line arrow function). Braces inside the
// parameter list (TS object types, JS default objects), strings and
// comments do not count. Rust skips only "..." strings, because ' also
// starts a lifetime; PHP also treats # as a line comment.
func extractBracedBody(body, lang string) string {
	depth, parens := 0, 0
	for i := 0; i < len(body); i++ {
		ch := body[i]
		switch {
		case ch == '/' && i+1 < len(body) && body[i+1] == '/',
			ch == '#' && lang == "php":
			for i < len(body) && body[i] != '\n' {
				i++
			}
			continue
		case ch == '/' && i+1 < len(body) && body[i+1] == '*':
			end := strings.Index(body[i+2:], "*/")
			if end < 0 {
				return body
			}
			i += 2 + end + 1
			continue
		case ch == '"' || (ch == '\'' && lang != "rust") || (ch == '`' && lang != "rust" && lang != "php"):
			i = skipQuoted(body, i)
			continue
		}
		switch ch {
		case '(':
			parens++
		case ')':
			parens--
		case '{':
			if parens <= 0 {
				depth++
			}
		case '}':
			if parens <= 0 {
				depth--
				if depth <= 0 {
					return body[:i+1]
				}
			}
		case ';':
			if depth == 0 && parens <= 0 {
				return body[:i+1]
			}
		}
	}
	return body
}

// skipQuoted returns the index of the quote that closes the string opened
// at body[start], honouring backslash escapes, or the last index when the
// string never closes.
func skipQuoted(body string, start int) int {
	quote := body[start]
	for i := start + 1; i < len(body); i++ {
		switch body[i] {
		case '\\':
			i++
		case quote:
			return i
		}
	}
	return len(body) - 1
}

// genericKeywords lists common keywords across JS/TS/Python/Rust
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

// phpKeywords are PHP language constructs that look like calls. They are
// checked only for PHP, so a JS or Rust function named list() or empty()
// keeps its edges.
var phpKeywords = map[string]bool{
	"array": true, "isset": true, "empty": true, "unset": true, "list": true,
	"echo": true, "foreach": true, "elseif": true, "exit": true, "die": true,
	"include": true, "include_once": true, "require_once": true, "declare": true,
}

// isKeyword reports whether a `name(` in this parser's language is a
// keyword or language construct rather than a call.
func (p *GenericParser) isKeyword(s string) bool {
	return genericKeywords[s] || (p.language == "php" && phpKeywords[s])
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
