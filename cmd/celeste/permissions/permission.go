// cmd/celeste/permissions/permission.go
package permissions

import (
	"fmt"
	"strings"
)

// Decision represents the outcome of a permission check.
type Decision int

const (
	// Allow means the tool execution is permitted without user interaction.
	Allow Decision = iota
	// Deny means the tool execution is blocked.
	Deny
	// Ask means the user must be prompted for approval.
	Ask
)

// String returns the human-readable name of the decision.
func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case Ask:
		return "ask"
	default:
		return "unknown"
	}
}

// PermissionMode controls the default behavior when no explicit rule matches.
type PermissionMode string

const (
	// ModeDefault auto-allows read-only tools, asks for writes.
	ModeDefault PermissionMode = "default"
	// ModeStrict asks the user for every tool invocation.
	ModeStrict PermissionMode = "strict"
	// ModeTrust auto-allows everything (current behavior pre-v1.7).
	ModeTrust PermissionMode = "trust"
)

// String returns the mode as a string.
func (m PermissionMode) String() string {
	return string(m)
}

// Valid returns true if the mode is one of the recognized values.
func (m PermissionMode) Valid() bool {
	switch m {
	case ModeDefault, ModeStrict, ModeTrust:
		return true
	default:
		return false
	}
}

// ParsePermissionMode parses a string into a PermissionMode.
// Matching is case-insensitive.
func ParsePermissionMode(s string) (PermissionMode, error) {
	lower := strings.ToLower(strings.TrimSpace(s))
	mode := PermissionMode(lower)
	if !mode.Valid() {
		return "", fmt.Errorf("invalid permission mode %q: must be one of default, strict, trust", s)
	}
	return mode, nil
}

// Rule defines a permission rule that matches a tool invocation pattern.
//
// ToolPattern supports:
//   - Exact tool name match: "read_file"
//   - Tool name with argument glob: "bash(git *)" — matches tool "bash" when the
//     first string argument (typically "command") matches the glob "git *"
//   - Wildcard tool: "*" — matches any tool
//
// InputPattern is an optional secondary pattern that matches against the
// JSON-serialized input. When empty, only ToolPattern is evaluated.
type Rule struct {
	// ToolPattern is the pattern to match against tool name and optionally its input.
	ToolPattern string `json:"tool_pattern"`

	// InputPattern is an optional glob matched against JSON-serialized input.
	InputPattern string `json:"input_pattern,omitempty"`

	// Decision is the action to take when this rule matches.
	Decision Decision `json:"decision"`
}

// CheckResult is the outcome of running an input through the permission checker.
type CheckResult struct {
	// Decision is the final permission decision.
	Decision Decision

	// MatchedRule is the rule that produced this decision, if any.
	// Nil when the decision comes from mode fallthrough.
	MatchedRule *Rule

	// Reason is a human-readable explanation of why this decision was made.
	Reason string
}

// IsAllowed returns true if the tool execution is permitted.
func (cr CheckResult) IsAllowed() bool {
	return cr.Decision == Allow
}

// IsDenied returns true if the tool execution is blocked.
func (cr CheckResult) IsDenied() bool {
	return cr.Decision == Deny
}

// NeedsPrompt returns true if the user must be prompted for approval.
func (cr CheckResult) NeedsPrompt() bool {
	return cr.Decision == Ask
}
