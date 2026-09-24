package builtin

import (
	"encoding/json"
	"strings"
)

// Some models double-escape JSON tool arguments, so a newline arrives as the
// two characters `\n`. Blindly unescaping every argument (as these tools used
// to) corrupts source code, where `\n` and `\t` inside string literals and
// regexes are real text (#165). These helpers only decode when the payload
// itself, or the file it must match, shows it was double-escaped.

// decodeDoubleEscaped reports whether s looks double-escaped and returns the
// decoded form. A payload with a real newline was not double-escaped, and one
// without a literal `\n` has nothing to recover, so both are returned as-is.
func decodeDoubleEscaped(s string) (string, bool) {
	if strings.Contains(s, "\n") || !strings.Contains(s, `\n`) {
		return s, false
	}
	return unescapeSequences(s), true
}

// matchNeedle returns the form of needle that occurs in haystack: needle
// itself when it matches verbatim, otherwise its unescaped form if that
// matches. The file decides, so a verbatim match is never rewritten.
func matchNeedle(haystack, needle string) (string, bool) {
	if strings.Contains(haystack, needle) {
		return needle, false
	}
	alt := unescapeSequences(needle)
	if alt != needle && strings.Contains(haystack, alt) {
		return alt, true
	}
	return needle, false
}

// unescapeSequences decodes s as the body of a JSON string, which handles
// `\\`, `\"` and `\uXXXX` alongside `\n` and `\t`. If s isn't a valid JSON
// string body (e.g. it holds a bare quote), only `\n` and `\t` are decoded.
func unescapeSequences(s string) string {
	var out string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &out); err == nil {
		return out
	}
	s = strings.ReplaceAll(s, `\n`, "\n")
	return strings.ReplaceAll(s, `\t`, "\t")
}
