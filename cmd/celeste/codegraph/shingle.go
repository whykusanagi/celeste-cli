package codegraph

import (
	"regexp"
	"strings"
	"unicode"
)

// ShinglesForSymbol generates enriched shingles for a symbol, used as input
// to MinHash for semantic similarity search. Each shingle is a lowercased token
// derived from the symbol's name, types, body references, package, and comments.
func ShinglesForSymbol(sym Symbol, source []byte) []string {
	var shingles []string

	// 1. Name parts (split camelCase/snake_case)
	shingles = append(shingles, splitIdentifier(sym.Name)...)

	// 2. Parameter/return types from signature
	if sym.Signature != "" {
		shingles = append(shingles, extractTypeTokens(sym.Signature)...)
	}

	// 3. Referenced identifiers in body (top 20 by frequency)
	if len(source) > 0 {
		shingles = append(shingles, extractBodyIdentifiers(source, sym)...)
	}

	// 4. Package name
	if sym.Package != "" {
		shingles = append(shingles, splitIdentifier(sym.Package)...)
	}

	// 5. Comment keywords (if present in source)
	if len(source) > 0 {
		shingles = append(shingles, extractCommentTokens(source, sym)...)
	}

	return deduplicateLowercase(shingles)
}

// splitIdentifier splits a camelCase, PascalCase, or snake_case identifier
// into its constituent words, all lowercased.
func splitIdentifier(name string) []string {
	// First handle snake_case
	if strings.Contains(name, "_") {
		parts := strings.Split(name, "_")
		var result []string
		for _, p := range parts {
			if p == "" {
				continue
			}
			// Recursively split camelCase parts
			result = append(result, splitCamelCase(p)...)
		}
		return lowercase(result)
	}
	return lowercase(splitCamelCase(name))
}

// splitCamelCase splits a camelCase or PascalCase string into words.
// "HTTPServer" -> ["HTTP", "Server"]
// "parseJSON" -> ["parse", "JSON"]
// "HTMLToMarkdown" -> ["HTML", "To", "Markdown"]
func splitCamelCase(s string) []string {
	if s == "" {
		return nil
	}

	runes := []rune(s)
	var words []string
	start := 0

	for i := 1; i < len(runes); i++ {
		// Transition: lower -> Upper marks a new word
		if unicode.IsLower(runes[i-1]) && unicode.IsUpper(runes[i]) {
			words = append(words, string(runes[start:i]))
			start = i
			continue
		}

		// Transition: Upper -> Upper -> lower marks end of acronym
		// "HTTPServer" at position of 'e' (after 'S'): "HTTP" + "Server"
		if i > 1 && unicode.IsUpper(runes[i-1]) && unicode.IsUpper(runes[i-2]) && unicode.IsLower(runes[i]) {
			words = append(words, string(runes[start:i-1]))
			start = i - 1
			continue
		}
	}

	// Add the last word
	if start < len(runes) {
		words = append(words, string(runes[start:]))
	}

	return words
}

// extractTypeTokens extracts identifier parts from a function signature.
func extractTypeTokens(signature string) []string {
	// Remove common syntax characters
	cleaned := strings.NewReplacer(
		"(", " ", ")", " ", ",", " ", "*", " ", "&", " ",
		"[", " ", "]", " ", "{", " ", "}", " ",
		"func ", " ", "...", " ",
	).Replace(signature)

	var tokens []string
	for _, word := range strings.Fields(cleaned) {
		// Split dotted names: http.ResponseWriter -> [http, ResponseWriter]
		parts := strings.Split(word, ".")
		for _, part := range parts {
			if part == "" {
				continue
			}
			tokens = append(tokens, splitIdentifier(part)...)
		}
	}
	return tokens
}

// identifierRegex matches Go-style identifiers.
var identifierRegex = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// extractBodyIdentifiers finds the most frequently referenced identifiers in
// a function body. Returns split parts of the top 20 identifiers.
func extractBodyIdentifiers(source []byte, sym Symbol) []string {
	// Find the approximate body of the symbol in source
	body := findSymbolBody(source, sym)
	if body == "" {
		return nil
	}

	// Count identifier frequencies
	freqs := make(map[string]int)
	matches := identifierRegex.FindAllString(body, -1)
	for _, m := range matches {
		if isKeyword(m) || len(m) <= 1 {
			continue
		}
		freqs[m]++
	}

	// Get top 20 by frequency
	type entry struct {
		name  string
		count int
	}
	var entries []entry
	for name, count := range freqs {
		entries = append(entries, entry{name, count})
	}
	// Simple selection sort for top 20 (small N)
	for i := 0; i < len(entries) && i < 20; i++ {
		maxIdx := i
		for j := i + 1; j < len(entries); j++ {
			if entries[j].count > entries[maxIdx].count {
				maxIdx = j
			}
		}
		entries[i], entries[maxIdx] = entries[maxIdx], entries[i]
	}

	limit := 20
	if len(entries) < limit {
		limit = len(entries)
	}

	var result []string
	for _, e := range entries[:limit] {
		result = append(result, splitIdentifier(e.name)...)
	}
	return result
}

// findSymbolBody extracts the approximate body text for a symbol from source.
func findSymbolBody(source []byte, sym Symbol) string {
	lines := strings.Split(string(source), "\n")
	if sym.Line <= 0 || sym.Line > len(lines) {
		return ""
	}

	// Start from the symbol's line, read until we hit a closing brace
	// at the same or lesser indentation, or for 50 lines max.
	start := sym.Line - 1
	end := start + 50
	if end > len(lines) {
		end = len(lines)
	}

	return strings.Join(lines[start:end], "\n")
}

// extractCommentTokens finds comment text near a symbol and extracts keywords.
func extractCommentTokens(source []byte, sym Symbol) []string {
	lines := strings.Split(string(source), "\n")
	if sym.Line <= 0 || sym.Line > len(lines) {
		return nil
	}

	// Look at lines immediately before the symbol for doc comments
	var commentLines []string
	for i := sym.Line - 2; i >= 0 && i >= sym.Line-5; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "//") {
			comment := strings.TrimPrefix(trimmed, "//")
			comment = strings.TrimSpace(comment)
			commentLines = append(commentLines, comment)
		} else if strings.HasPrefix(trimmed, "#") {
			comment := strings.TrimPrefix(trimmed, "#")
			comment = strings.TrimSpace(comment)
			commentLines = append(commentLines, comment)
		} else {
			break
		}
	}

	var tokens []string
	for _, line := range commentLines {
		words := strings.Fields(line)
		for _, w := range words {
			// Remove punctuation
			cleaned := strings.Trim(w, ".,;:!?()[]{}\"'`")
			if len(cleaned) > 2 && !isKeyword(cleaned) {
				tokens = append(tokens, cleaned)
			}
		}
	}
	return tokens
}

// keywords contains common language keywords that should not be used as shingles.
var keywords = map[string]bool{
	"func": true, "return": true, "if": true, "else": true, "for": true,
	"range": true, "var": true, "const": true, "type": true, "struct": true,
	"interface": true, "package": true, "import": true, "switch": true,
	"case": true, "default": true, "break": true, "continue": true,
	"defer": true, "go": true, "select": true, "chan": true, "map": true,
	"make": true, "new": true, "nil": true, "true": true, "false": true,
	"def": true, "class": true, "self": true, "from": true, "with": true,
	"as": true, "in": true, "not": true, "and": true, "or": true,
	"function": true, "let": true, "this": true, "async": true, "await": true,
	"pub": true, "use": true, "mod": true, "impl": true, "trait": true,
	"the": true, "is": true, "are": true, "was": true, "been": true,
}

func isKeyword(s string) bool {
	return keywords[strings.ToLower(s)]
}

func lowercase(ss []string) []string {
	result := make([]string, len(ss))
	for i, s := range ss {
		result[i] = strings.ToLower(s)
	}
	return result
}

func deduplicateLowercase(ss []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		lower := strings.ToLower(s)
		if !seen[lower] && lower != "" {
			seen[lower] = true
			result = append(result, lower)
		}
	}
	return result
}
