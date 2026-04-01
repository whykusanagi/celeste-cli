// cmd/celeste/permissions/rules.go
package permissions

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// MatchRule returns true if the given tool invocation matches the rule's patterns.
//
// The matching process:
//  1. Parse ToolPattern into tool name and optional argument glob.
//  2. Match tool name: exact match or "*" wildcard.
//  3. If argument glob is present, extract the first string argument from input
//     (checking "command", then "path", then first string value found) and match
//     using filepath.Match semantics.
//  4. If InputPattern is set, JSON-serialize the input and match against it using
//     filepath.Match.
//  5. All applicable patterns must match for the rule to match.
func MatchRule(rule Rule, toolName string, input map[string]any) bool {
	// Step 1: Parse tool pattern
	patternTool, argGlob := ParseToolPattern(rule.ToolPattern)

	// Step 2: Match tool name
	if patternTool != "*" && patternTool != toolName {
		return false
	}

	// Step 3: Match argument glob if present
	if argGlob != "" {
		firstArg := ExtractFirstStringArg(input)
		if firstArg == "" {
			return false
		}
		if !globMatch(argGlob, firstArg) {
			return false
		}
	}

	// Step 4: Match input pattern if present
	if rule.InputPattern != "" {
		if input == nil {
			return false
		}
		serialized, err := json.Marshal(input)
		if err != nil {
			return false
		}
		if !globMatch(rule.InputPattern, string(serialized)) {
			return false
		}
	}

	return true
}

// ParseToolPattern splits a tool pattern into the tool name and an optional
// argument glob. For example:
//
//	"bash(git *)" -> ("bash", "git *")
//	"read_file"   -> ("read_file", "")
//	"*"           -> ("*", "")
func ParseToolPattern(pattern string) (toolName string, argGlob string) {
	idx := strings.IndexByte(pattern, '(')
	if idx < 0 {
		return pattern, ""
	}
	toolName = pattern[:idx]
	rest := pattern[idx+1:]
	// Strip trailing ')'
	rest = strings.TrimSuffix(rest, ")")
	return toolName, rest
}

// ExtractFirstStringArg extracts the primary string argument from a tool input
// map. It checks keys in priority order: "command", "path", then returns the
// first string value found by iterating the map.
func ExtractFirstStringArg(input map[string]any) string {
	if input == nil {
		return ""
	}

	// Priority keys
	for _, key := range []string{"command", "path", "content", "pattern"} {
		if v, ok := input[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}

	// Fallback: first string value found
	for _, v := range input {
		if s, ok := v.(string); ok {
			return s
		}
	}

	return ""
}

// globMatch performs a glob match that handles patterns with spaces and
// multi-segment arguments. Standard filepath.Match only handles single path
// segments, so we use a custom approach for patterns containing spaces.
//
// For patterns like "git *", we check if the string starts with the prefix
// before the "*". For patterns without "*", we do exact match.
func globMatch(pattern, s string) bool {
	// Fast path: try filepath.Match first (works for simple patterns)
	if matched, err := filepath.Match(pattern, s); err == nil && matched {
		return true
	}

	// For patterns with *, do prefix/suffix/contains matching
	if strings.Contains(pattern, "*") {
		return globMatchWildcard(pattern, s)
	}

	// Exact match
	return pattern == s
}

// globMatchWildcard handles patterns with * wildcards for multi-word strings.
// Supports patterns like:
//   - "git *"       -> matches anything starting with "git "
//   - "*secret*"    -> matches anything containing "secret"
//   - "rm -rf *"    -> matches anything starting with "rm -rf "
func globMatchWildcard(pattern, s string) bool {
	// Split on * and check that all parts appear in order
	parts := strings.Split(pattern, "*")

	remaining := s
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(remaining, part)
		if idx < 0 {
			return false
		}
		// First part must be a prefix
		if i == 0 && idx != 0 {
			return false
		}
		remaining = remaining[idx+len(part):]
	}

	// If pattern doesn't end with *, the remaining string must be empty
	if !strings.HasSuffix(pattern, "*") && remaining != "" {
		return false
	}

	return true
}
