// cmd/celeste/permissions/rules.go
package permissions

import (
	"encoding/json"
	"path/filepath"
	"regexp"
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
	return matchRule(rule, toolName, argLegacy, input, false, nil)
}

// argLegacy as matchRule's primary means the tool names no primary
// argument mechanism at all: the rule's argument is ExtractFirstStringArg's.
const argLegacy = "\x00legacy"

// PrimaryArger is implemented by a ToolInfo that names the input field an
// argument-scoped rule ("write_file(src/*)") is matched against: the field
// the tool acts on (bash: command, the file tools: path).
type PrimaryArger interface {
	PrimaryArg() string
}

// primaryArgOf is tool's primary argument field: "" when it has none,
// argLegacy when tool is not a PrimaryArger.
func primaryArgOf(tool ToolInfo) string {
	if pa, ok := tool.(PrimaryArger); ok {
		return pa.PrimaryArg()
	}
	return argLegacy
}

// matchRule is MatchRule for a tool whose primary argument field is
// primary. An argument glob is matched against that field only, so an
// extra field the tool never reads cannot decide the rule; when the tool
// has no primary field ("") or the call lacks it, a restricting rule
// (restricting: a deny or ask) still matches and a permitting one does
// not. With primary argLegacy, the argument is ExtractFirstStringArg's.
// argMatches decides the glob itself: paths are cleaned first (and, with a
// workspace ws, also matched relative to it) and command lines are matched
// command by command.
func matchRule(rule Rule, toolName, primary string, input map[string]any, restricting bool, ws *workspace) bool {
	// Step 1: Parse tool pattern
	patternTool, argGlob := ParseToolPattern(rule.ToolPattern)

	// Step 2: Match tool name
	if patternTool != "*" && patternTool != toolName {
		return false
	}

	// Step 3: Match argument glob if present
	if argGlob != "" {
		var arg, key string
		switch primary {
		case argLegacy:
			arg, key = extractFirstStringArg(input)
		case "":
		default:
			arg, _ = input[primary].(string)
			key = primary
		}
		if arg == "" {
			if !restricting || primary == argLegacy {
				return false
			}
		} else if !argMatches(argGlob, key, arg, restricting, ws) {
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
	s, _ := extractFirstStringArg(input)
	return s
}

// extractFirstStringArg is ExtractFirstStringArg with the key it read.
func extractFirstStringArg(input map[string]any) (string, string) {
	if input == nil {
		return "", ""
	}

	// Priority keys
	for _, key := range []string{"command", "path", "content", "pattern"} {
		if v, ok := input[key]; ok {
			if s, ok := v.(string); ok {
				return s, key
			}
		}
	}

	// Fallback: first string value found
	for k, v := range input {
		if s, ok := v.(string); ok {
			return s, k
		}
	}

	return "", ""
}

// pathArgs are the input fields that hold a file path.
var pathArgs = map[string]bool{"path": true, "file_path": true, "dir": true, "directory": true}

// argMatches matches an argument glob against the value of input field
// key. A path is cleaned first, so ".." can neither take an allowed path
// out of the rule's directory nor a denied one past it; a cleaned path that
// still climbs out ("../x") is never permitted. With the workspace known,
// a path is also matched as the workspace-relative path it names, lexically
// and with symlinks resolved (pathForms): a restricting rule matches when
// any spelling does, a permitting one only when every one does. A command
// line is matched one command at a time: a permitting rule never matches a
// line that chains, pipes, substitutes or redirects (shellOperator), and a
// restricting rule matches when any one command in it matches, also with
// leading shell keywords, wrappers and assignments taken off
// (commandWord). That is best-effort: a shell can spell a command in ways
// no glob sees, so the sandbox and hooks are the boundary, not deny rules.
func argMatches(glob, key, arg string, restricting bool, ws *workspace) bool {
	switch {
	case pathArgs[key]:
		for _, p := range pathForms(arg, ws, restricting) {
			climbs := p == ".." || strings.HasPrefix(p, "../")
			hit := globMatch(glob, p)
			if restricting && hit {
				return true
			}
			if !restricting && (climbs || !hit) {
				return false
			}
		}
		return !restricting
	case key == "command":
		if !restricting {
			return !shellOperator.MatchString(arg) && globMatch(glob, strings.TrimSpace(arg))
		}
		if globMatch(glob, arg) {
			return true
		}
		for _, seg := range shellOperator.Split(arg, -1) {
			if seg = strings.TrimSpace(seg); seg == "" {
				continue
			}
			if globMatch(glob, seg) || globMatch(glob, commandWord(seg)) {
				return true
			}
		}
		return false
	}
	return globMatch(glob, arg)
}

// shellPrefixes are words that run the command after them: shell keywords
// that start a command list, and wrappers that run their arguments.
var shellPrefixes = map[string]bool{
	"{": true, "}": true, "!": true, "if": true, "then": true, "elif": true, "else": true,
	"do": true, "while": true, "until": true, "command": true, "builtin": true,
	"exec": true, "env": true, "nohup": true, "time": true,
}

// assignment is a leading VAR=value word.
var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// commandWord is the command seg runs, best-effort: leading shell keywords,
// wrappers (and their -options), VAR=value assignments, a backslash and
// quotes around the command name are taken off ("x=1 command \rm y" is
// "rm y").
func commandWord(seg string) string {
	wrapped := false
	for {
		seg = strings.TrimLeft(seg, " \t")
		end := strings.IndexAny(seg, " \t")
		if end < 0 {
			end = len(seg)
		}
		w := seg[:end]
		switch {
		case w == "":
			return seg
		case shellPrefixes[w]:
			wrapped = true
		case assignment.MatchString(w):
		case wrapped && strings.HasPrefix(w, "-"):
		default:
			name := strings.TrimLeft(w, "\\")
			name = strings.NewReplacer(`'`, "", `"`, "").Replace(name)
			return name + seg[end:]
		}
		seg = seg[end:]
	}
}

// shellOperator finds what makes one command line run more than one
// command or write somewhere: ; & | (and && ||), a newline, backquotes,
// ( ) (so $( and subshells), and < > redirections.
var shellOperator = regexp.MustCompile("[;&|\n\r`<>()]")

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
