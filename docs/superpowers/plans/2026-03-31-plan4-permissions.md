# Plan 4: Permission & Safety System

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Multi-layer permission system with configurable rules, denial tracking, and persistent configuration. Replaces the hardcoded safety checks scattered across `agent/dev_skills.go` (now `tools/builtin/*.go` after Plan 1) with a centralized, configurable permission checker.

**Architecture:** New `permissions/` package under `cmd/celeste/` provides a `Checker` that evaluates tool+input against layered rules. Every tool execution passes through the permission checker before running. The evaluation chain is: alwaysDeny -> alwaysAllow -> IsReadOnly check -> pattern rules -> mode fallthrough.

**Tech Stack:** Go 1.26, standard library, `filepath.Match` for glob patterns

**Prerequisite Plans:** Plan 1 (Unified Tool Layer)

---

## Codebase Context

**Current safety mechanisms (being replaced):**

1. **`cmd/celeste/agent/dev_skills.go`** (deleted in Plan 1, logic moved to `tools/builtin/`)
   - Hardcoded `sudo`/`su` blocking in `devRunCommandHandler` (line 540-542)
   - Path traversal prevention via `resolveWorkspacePath` (`..` checks, line 608-629)
   - File read limits: `maxReadBytes = 200_000` (line 19)
   - Command output truncation: `maxCommandOutput = 12_000` (line 20)

2. **After Plan 1**, these checks live inside individual tools (`tools/builtin/bash.go`, etc.) but remain hardcoded — no way to configure, override, or extend them.

**What Plan 1 provides (dependencies):**

- `cmd/celeste/tools/tool.go` — `Tool` interface with `IsReadOnly() bool`, `IsConcurrencySafe()`, `Execute()`, `Name() string`
- `cmd/celeste/tools/registry.go` — `Registry` with mode filtering, `Register()`, `Get()`, `List()`
- `cmd/celeste/tools/builtin/*.go` — all tool implementations with hardcoded safety checks

**Module path:** `github.com/whykusanagi/celeste-cli`

---

## File Structure

```
cmd/celeste/permissions/                # NEW package
├── permission.go                       # Core types: Decision, PermissionMode, Rule, CheckResult
├── permission_test.go                  # Tests for core types
├── rules.go                           # Pattern matching: MatchRule()
├── rules_test.go                      # Tests for pattern matching
├── checker.go                         # Checker with 5-step evaluation chain
├── checker_test.go                    # Tests for each evaluation step
├── denial.go                          # DenialTracker for consecutive denial tracking
├── denial_test.go                     # Tests for denial tracking
├── config.go                          # PermissionConfig, LoadConfig, SaveConfig
├── config_test.go                     # Tests for persistent config
```

**Modified files (integration):**
- `cmd/celeste/tools/registry.go` — add `SetPermissionChecker()`, permission check before execution
- `cmd/celeste/main.go` — load config on startup, create Checker, wire into Registry

---

### Task 1: Core Types

**Files:**
- Create: `cmd/celeste/permissions/permission.go`
- Test: `cmd/celeste/permissions/permission_test.go`

- [ ] **Step 1: Write the test for core types**

```go
// cmd/celeste/permissions/permission_test.go
package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecision_String(t *testing.T) {
	tests := []struct {
		d    Decision
		want string
	}{
		{Allow, "allow"},
		{Deny, "deny"},
		{Ask, "ask"},
		{Decision(99), "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.d.String())
	}
}

func TestPermissionMode_String(t *testing.T) {
	tests := []struct {
		m    PermissionMode
		want string
	}{
		{ModeDefault, "default"},
		{ModeStrict, "strict"},
		{ModeTrust, "trust"},
		{PermissionMode("bogus"), "bogus"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.m.String())
	}
}

func TestPermissionMode_Valid(t *testing.T) {
	assert.True(t, ModeDefault.Valid())
	assert.True(t, ModeStrict.Valid())
	assert.True(t, ModeTrust.Valid())
	assert.False(t, PermissionMode("yolo").Valid())
}

func TestParsePermissionMode(t *testing.T) {
	tests := []struct {
		input string
		want  PermissionMode
		ok    bool
	}{
		{"default", ModeDefault, true},
		{"strict", ModeStrict, true},
		{"trust", ModeTrust, true},
		{"DEFAULT", ModeDefault, true},
		{"Trust", ModeTrust, true},
		{"invalid", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, err := ParsePermissionMode(tt.input)
		if tt.ok {
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		} else {
			assert.Error(t, err)
		}
	}
}

func TestRule_Fields(t *testing.T) {
	r := Rule{
		ToolPattern:  "bash(git *)",
		InputPattern: "",
		Decision:     Allow,
	}
	assert.Equal(t, "bash(git *)", r.ToolPattern)
	assert.Equal(t, Allow, r.Decision)
	assert.Empty(t, r.InputPattern)
}

func TestCheckResult_IsAllowed(t *testing.T) {
	assert.True(t, CheckResult{Decision: Allow}.IsAllowed())
	assert.False(t, CheckResult{Decision: Deny}.IsAllowed())
	assert.False(t, CheckResult{Decision: Ask}.IsAllowed())
}

func TestCheckResult_IsDenied(t *testing.T) {
	assert.True(t, CheckResult{Decision: Deny}.IsDenied())
	assert.False(t, CheckResult{Decision: Allow}.IsDenied())
	assert.False(t, CheckResult{Decision: Ask}.IsDenied())
}

func TestCheckResult_NeedsPrompt(t *testing.T) {
	assert.True(t, CheckResult{Decision: Ask}.NeedsPrompt())
	assert.False(t, CheckResult{Decision: Allow}.NeedsPrompt())
	assert.False(t, CheckResult{Decision: Deny}.NeedsPrompt())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -run TestDecision_String`
Expected: FAIL -- package doesn't exist yet

- [ ] **Step 3: Write the core types**

```go
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
```

- [ ] **Step 4: Run tests, verify green**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v`
Expected: All tests PASS

---

### Task 2: Pattern Matching

**Files:**
- Create: `cmd/celeste/permissions/rules.go`
- Test: `cmd/celeste/permissions/rules_test.go`

- [ ] **Step 1: Write the test for pattern matching**

```go
// cmd/celeste/permissions/rules_test.go
package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchRule_ExactToolName(t *testing.T) {
	rule := Rule{ToolPattern: "read_file", Decision: Allow}
	assert.True(t, MatchRule(rule, "read_file", nil))
	assert.False(t, MatchRule(rule, "write_file", nil))
	assert.False(t, MatchRule(rule, "READ_FILE", nil))
}

func TestMatchRule_WildcardTool(t *testing.T) {
	rule := Rule{ToolPattern: "*", Decision: Deny}
	assert.True(t, MatchRule(rule, "read_file", nil))
	assert.True(t, MatchRule(rule, "bash", nil))
	assert.True(t, MatchRule(rule, "anything", nil))
}

func TestMatchRule_ToolWithArgumentGlob(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		toolName string
		input    map[string]any
		want     bool
	}{
		{
			name:     "bash git star matches git status",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    map[string]any{"command": "git status"},
			want:     true,
		},
		{
			name:     "bash git star matches git commit with flags",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    map[string]any{"command": "git commit -m 'hello'"},
			want:     true,
		},
		{
			name:     "bash git star does not match ls",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    map[string]any{"command": "ls -la"},
			want:     false,
		},
		{
			name:     "bash rm -rf star matches rm -rf /tmp",
			pattern:  "bash(rm -rf *)",
			toolName: "bash",
			input:    map[string]any{"command": "rm -rf /tmp/foo"},
			want:     true,
		},
		{
			name:     "bash rm -rf star does not match rm file",
			pattern:  "bash(rm -rf *)",
			toolName: "bash",
			input:    map[string]any{"command": "rm file.txt"},
			want:     false,
		},
		{
			name:     "bash sudo star matches sudo apt",
			pattern:  "bash(sudo *)",
			toolName: "bash",
			input:    map[string]any{"command": "sudo apt install vim"},
			want:     true,
		},
		{
			name:     "wrong tool name does not match",
			pattern:  "bash(git *)",
			toolName: "read_file",
			input:    map[string]any{"command": "git status"},
			want:     false,
		},
		{
			name:     "no input returns false for argument glob",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    nil,
			want:     false,
		},
		{
			name:     "empty command returns false",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    map[string]any{"command": ""},
			want:     false,
		},
		{
			name:     "non-string command returns false",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    map[string]any{"command": 42},
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := Rule{ToolPattern: tt.pattern, Decision: Allow}
			assert.Equal(t, tt.want, MatchRule(rule, tt.toolName, tt.input))
		})
	}
}

func TestMatchRule_InputPattern(t *testing.T) {
	tests := []struct {
		name         string
		toolPattern  string
		inputPattern string
		toolName     string
		input        map[string]any
		want         bool
	}{
		{
			name:         "input pattern matches path field",
			toolPattern:  "read_file",
			inputPattern: "*/secret*",
			toolName:     "read_file",
			input:        map[string]any{"path": "/etc/secret.key"},
			want:         true,
		},
		{
			name:         "input pattern does not match",
			toolPattern:  "read_file",
			inputPattern: "*/secret*",
			toolName:     "read_file",
			input:        map[string]any{"path": "/etc/hosts"},
			want:         false,
		},
		{
			name:         "tool pattern fails so input pattern skipped",
			toolPattern:  "write_file",
			inputPattern: "*secret*",
			toolName:     "read_file",
			input:        map[string]any{"path": "/etc/secret.key"},
			want:         false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := Rule{ToolPattern: tt.toolPattern, InputPattern: tt.inputPattern, Decision: Allow}
			assert.Equal(t, tt.want, MatchRule(rule, tt.toolName, tt.input))
		})
	}
}

func TestParseToolPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		wantTool string
		wantArg  string
	}{
		{"bash(git *)", "bash", "git *"},
		{"read_file", "read_file", ""},
		{"*", "*", ""},
		{"bash(sudo *)", "bash", "sudo *"},
		{"bash(rm -rf *)", "bash", "rm -rf *"},
		{"bash()", "bash", ""},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			tool, arg := ParseToolPattern(tt.pattern)
			assert.Equal(t, tt.wantTool, tool)
			assert.Equal(t, tt.wantArg, arg)
		})
	}
}

func TestExtractFirstStringArg(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{
			name:  "command key",
			input: map[string]any{"command": "git status"},
			want:  "git status",
		},
		{
			name:  "path key when no command",
			input: map[string]any{"path": "/etc/hosts"},
			want:  "/etc/hosts",
		},
		{
			name:  "command takes priority over path",
			input: map[string]any{"command": "ls", "path": "/tmp"},
			want:  "ls",
		},
		{
			name:  "nil input",
			input: nil,
			want:  "",
		},
		{
			name:  "empty map",
			input: map[string]any{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExtractFirstStringArg(tt.input))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -run TestMatchRule`
Expected: FAIL -- functions don't exist yet

- [ ] **Step 3: Write the pattern matching implementation**

```go
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
```

- [ ] **Step 4: Run tests, verify green**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -run "TestMatchRule|TestParseToolPattern|TestExtractFirstStringArg"`
Expected: All tests PASS

---

### Task 3: Checker (5-Step Evaluation Chain)

**Files:**
- Create: `cmd/celeste/permissions/checker.go`
- Test: `cmd/celeste/permissions/checker_test.go`

- [ ] **Step 1: Write the test for the checker**

```go
// cmd/celeste/permissions/checker_test.go
package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// toolStub implements the minimal interface the Checker needs from a tool.
// We define it here to avoid importing the tools package (avoiding circular deps).
type toolStub struct {
	name     string
	readOnly bool
}

func (t *toolStub) ToolName() string  { return t.name }
func (t *toolStub) IsReadOnly() bool  { return t.readOnly }

func readOnlyTool(name string) ToolInfo {
	return &toolStub{name: name, readOnly: true}
}

func writeTool(name string) ToolInfo {
	return &toolStub{name: name, readOnly: false}
}

// --- Step 1: AlwaysDeny takes highest priority ---

func TestChecker_AlwaysDenyBlocksEvenInTrustMode(t *testing.T) {
	checker := NewChecker(PermissionConfig{
		Mode: ModeTrust,
		AlwaysDeny: []Rule{
			{ToolPattern: "bash(sudo *)", Decision: Deny},
		},
	})

	result := checker.Check(writeTool("bash"), map[string]any{"command": "sudo rm -rf /"})
	assert.True(t, result.IsDenied())
	assert.Contains(t, result.Reason, "always-deny")
}

func TestChecker_AlwaysDenyNoMatchFallsThrough(t *testing.T) {
	checker := NewChecker(PermissionConfig{
		Mode: ModeTrust,
		AlwaysDeny: []Rule{
			{ToolPattern: "bash(sudo *)", Decision: Deny},
		},
	})

	result := checker.Check(writeTool("bash"), map[string]any{"command": "git status"})
	assert.True(t, result.IsAllowed(), "trust mode should allow when no deny rule matches")
}

// --- Step 2: AlwaysAllow overrides mode ---

func TestChecker_AlwaysAllowOverridesStrictMode(t *testing.T) {
	checker := NewChecker(PermissionConfig{
		Mode: ModeStrict,
		AlwaysAllow: []Rule{
			{ToolPattern: "bash(git *)", Decision: Allow},
		},
	})

	result := checker.Check(writeTool("bash"), map[string]any{"command": "git status"})
	assert.True(t, result.IsAllowed())
	assert.Contains(t, result.Reason, "always-allow")
}

// --- Step 3: IsReadOnly check in default mode ---

func TestChecker_DefaultModeAutoAllowsReadOnly(t *testing.T) {
	checker := NewChecker(PermissionConfig{Mode: ModeDefault})

	result := checker.Check(readOnlyTool("read_file"), map[string]any{"path": "/tmp/foo"})
	assert.True(t, result.IsAllowed())
	assert.Contains(t, result.Reason, "read-only")
}

func TestChecker_DefaultModeAsksForWriteTools(t *testing.T) {
	checker := NewChecker(PermissionConfig{Mode: ModeDefault})

	result := checker.Check(writeTool("write_file"), map[string]any{"path": "/tmp/foo", "content": "x"})
	assert.True(t, result.NeedsPrompt())
	assert.Contains(t, result.Reason, "default mode")
}

// --- Step 4: StrictMode asks for everything ---

func TestChecker_StrictModeAsksForReadOnly(t *testing.T) {
	checker := NewChecker(PermissionConfig{Mode: ModeStrict})

	result := checker.Check(readOnlyTool("read_file"), map[string]any{"path": "/tmp/foo"})
	assert.True(t, result.NeedsPrompt())
	assert.Contains(t, result.Reason, "strict mode")
}

func TestChecker_StrictModeAsksForWriteTools(t *testing.T) {
	checker := NewChecker(PermissionConfig{Mode: ModeStrict})

	result := checker.Check(writeTool("bash"), map[string]any{"command": "ls"})
	assert.True(t, result.NeedsPrompt())
}

// --- Step 5: TrustMode allows everything ---

func TestChecker_TrustModeAllowsEverything(t *testing.T) {
	checker := NewChecker(PermissionConfig{Mode: ModeTrust})

	result := checker.Check(writeTool("bash"), map[string]any{"command": "rm -rf /"})
	assert.True(t, result.IsAllowed())
	assert.Contains(t, result.Reason, "trust mode")
}

// --- Evaluation order ---

func TestChecker_DenyTakesPriorityOverAllow(t *testing.T) {
	checker := NewChecker(PermissionConfig{
		Mode: ModeTrust,
		AlwaysDeny: []Rule{
			{ToolPattern: "bash(sudo *)", Decision: Deny},
		},
		AlwaysAllow: []Rule{
			{ToolPattern: "bash(sudo *)", Decision: Allow},
		},
	})

	result := checker.Check(writeTool("bash"), map[string]any{"command": "sudo ls"})
	assert.True(t, result.IsDenied(), "deny must take priority over allow")
}

// --- Pattern rule matching ---

func TestChecker_PatternRulesEvaluatedBeforeModeFallthrough(t *testing.T) {
	checker := NewChecker(PermissionConfig{
		Mode: ModeDefault,
		PatternRules: []Rule{
			{ToolPattern: "write_file", Decision: Allow},
		},
	})

	result := checker.Check(writeTool("write_file"), map[string]any{"path": "/tmp/foo", "content": "x"})
	assert.True(t, result.IsAllowed())
	assert.Contains(t, result.Reason, "pattern rule")
}

func TestChecker_PatternRuleDenyOverridesModeFallthrough(t *testing.T) {
	checker := NewChecker(PermissionConfig{
		Mode: ModeTrust,
		PatternRules: []Rule{
			{ToolPattern: "bash(rm *)", Decision: Deny},
		},
	})

	// PatternRules are checked AFTER alwaysDeny and alwaysAllow, but BEFORE mode fallthrough.
	// However, pattern rules with Deny decision should still deny.
	result := checker.Check(writeTool("bash"), map[string]any{"command": "rm -rf /"})
	assert.True(t, result.IsDenied())
}

// --- Default config ---

func TestChecker_DefaultConfigBlocksSudo(t *testing.T) {
	cfg := DefaultConfig()
	checker := NewChecker(cfg)

	result := checker.Check(writeTool("bash"), map[string]any{"command": "sudo apt install vim"})
	assert.True(t, result.IsDenied())
}

func TestChecker_DefaultConfigBlocksSu(t *testing.T) {
	cfg := DefaultConfig()
	checker := NewChecker(cfg)

	result := checker.Check(writeTool("bash"), map[string]any{"command": "su root"})
	assert.True(t, result.IsDenied())
}

func TestChecker_NilToolInfo(t *testing.T) {
	checker := NewChecker(PermissionConfig{Mode: ModeDefault})

	// Should not panic with nil ToolInfo
	result := checker.Check(nil, map[string]any{"command": "ls"})
	assert.True(t, result.NeedsPrompt(), "nil tool treated as non-read-only, default mode asks")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -run TestChecker`
Expected: FAIL -- Checker doesn't exist yet

- [ ] **Step 3: Write the checker implementation**

```go
// cmd/celeste/permissions/checker.go
package permissions

import "fmt"

// ToolInfo is the minimal interface the Checker needs from a tool.
// This avoids importing the tools package (preventing circular dependencies).
// The tools.Tool interface satisfies this via its Name() and IsReadOnly() methods.
type ToolInfo interface {
	ToolName() string
	IsReadOnly() bool
}

// Checker evaluates whether a tool execution should be allowed, denied, or
// requires user approval. It implements a 5-step evaluation chain:
//
//  1. alwaysDeny rules — if any match, return Deny immediately
//  2. alwaysAllow rules — if any match, return Allow immediately
//  3. IsReadOnly check — in default mode, read-only tools are auto-allowed
//  4. patternRules — if any match, return the rule's decision
//  5. Mode fallthrough — default asks for writes, strict asks for all, trust allows all
type Checker struct {
	alwaysDeny   []Rule
	alwaysAllow  []Rule
	patternRules []Rule
	mode         PermissionMode
}

// NewChecker creates a Checker from a PermissionConfig.
func NewChecker(config PermissionConfig) *Checker {
	mode := config.Mode
	if !mode.Valid() {
		mode = ModeDefault
	}

	return &Checker{
		alwaysDeny:   config.AlwaysDeny,
		alwaysAllow:  config.AlwaysAllow,
		patternRules: config.PatternRules,
		mode:         mode,
	}
}

// Check evaluates whether the given tool invocation is permitted.
//
// The tool parameter provides tool metadata (name, read-only status).
// The input parameter contains the tool's input arguments.
//
// If tool is nil, it is treated as a non-read-only tool with an empty name.
func (c *Checker) Check(tool ToolInfo, input map[string]any) CheckResult {
	toolName := ""
	readOnly := false
	if tool != nil {
		toolName = tool.ToolName()
		readOnly = tool.IsReadOnly()
	}

	// Step 1: alwaysDeny rules (highest priority)
	for i := range c.alwaysDeny {
		if MatchRule(c.alwaysDeny[i], toolName, input) {
			return CheckResult{
				Decision:    Deny,
				MatchedRule: &c.alwaysDeny[i],
				Reason:      fmt.Sprintf("blocked by always-deny rule: %s", c.alwaysDeny[i].ToolPattern),
			}
		}
	}

	// Step 2: alwaysAllow rules
	for i := range c.alwaysAllow {
		if MatchRule(c.alwaysAllow[i], toolName, input) {
			return CheckResult{
				Decision:    Allow,
				MatchedRule: &c.alwaysAllow[i],
				Reason:      fmt.Sprintf("permitted by always-allow rule: %s", c.alwaysAllow[i].ToolPattern),
			}
		}
	}

	// Step 3: IsReadOnly check (only in default mode)
	if c.mode == ModeDefault && readOnly {
		return CheckResult{
			Decision: Allow,
			Reason:   fmt.Sprintf("read-only tool %q auto-allowed in default mode", toolName),
		}
	}

	// Step 4: Pattern rules
	for i := range c.patternRules {
		if MatchRule(c.patternRules[i], toolName, input) {
			return CheckResult{
				Decision:    c.patternRules[i].Decision,
				MatchedRule: &c.patternRules[i],
				Reason:      fmt.Sprintf("matched pattern rule: %s", c.patternRules[i].ToolPattern),
			}
		}
	}

	// Step 5: Mode fallthrough
	switch c.mode {
	case ModeTrust:
		return CheckResult{
			Decision: Allow,
			Reason:   fmt.Sprintf("trust mode: auto-allowing %q", toolName),
		}
	case ModeStrict:
		return CheckResult{
			Decision: Ask,
			Reason:   fmt.Sprintf("strict mode: asking for %q", toolName),
		}
	default: // ModeDefault
		if readOnly {
			return CheckResult{
				Decision: Allow,
				Reason:   fmt.Sprintf("read-only tool %q auto-allowed in default mode", toolName),
			}
		}
		return CheckResult{
			Decision: Ask,
			Reason:   fmt.Sprintf("default mode: asking for non-read-only tool %q", toolName),
		}
	}
}

// Mode returns the current permission mode.
func (c *Checker) Mode() PermissionMode {
	return c.mode
}
```

- [ ] **Step 4: Run tests, verify green**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -run TestChecker`
Expected: All tests PASS

---

### Task 4: Denial Tracking

**Files:**
- Create: `cmd/celeste/permissions/denial.go`
- Test: `cmd/celeste/permissions/denial_test.go`

- [ ] **Step 1: Write the test for denial tracking**

```go
// cmd/celeste/permissions/denial_test.go
package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDenialTracker_RecordAndCount(t *testing.T) {
	tracker := NewDenialTracker()

	assert.Equal(t, 0, tracker.GetDenialCount("bash"))
	assert.Equal(t, 0, tracker.GetTotalDenials())

	tracker.RecordDenial("bash")
	assert.Equal(t, 1, tracker.GetDenialCount("bash"))
	assert.Equal(t, 1, tracker.GetTotalDenials())

	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	assert.Equal(t, 3, tracker.GetDenialCount("bash"))
	assert.Equal(t, 3, tracker.GetTotalDenials())
}

func TestDenialTracker_MultipleTtools(t *testing.T) {
	tracker := NewDenialTracker()

	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	tracker.RecordDenial("write_file")

	assert.Equal(t, 2, tracker.GetDenialCount("bash"))
	assert.Equal(t, 1, tracker.GetDenialCount("write_file"))
	assert.Equal(t, 3, tracker.GetTotalDenials())
}

func TestDenialTracker_ShouldSuggestRule(t *testing.T) {
	tracker := NewDenialTracker()

	// Should not suggest before 3 denials
	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	assert.False(t, tracker.ShouldSuggestRule("bash"))

	// Should suggest at exactly 3
	tracker.RecordDenial("bash")
	assert.True(t, tracker.ShouldSuggestRule("bash"))

	// Should still suggest after 3
	tracker.RecordDenial("bash")
	assert.True(t, tracker.ShouldSuggestRule("bash"))

	// Different tool should not be affected
	assert.False(t, tracker.ShouldSuggestRule("write_file"))
}

func TestDenialTracker_ShouldSuggestStrictMode(t *testing.T) {
	tracker := NewDenialTracker()

	// Should not suggest before 5 total denials
	for i := 0; i < 4; i++ {
		tracker.RecordDenial("tool" + string(rune('A'+i)))
	}
	assert.False(t, tracker.ShouldSuggestStrictMode())

	// Should suggest at exactly 5
	tracker.RecordDenial("toolE")
	assert.True(t, tracker.ShouldSuggestStrictMode())

	// Should still suggest after 5
	tracker.RecordDenial("toolF")
	assert.True(t, tracker.ShouldSuggestStrictMode())
}

func TestDenialTracker_Reset(t *testing.T) {
	tracker := NewDenialTracker()

	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	assert.Equal(t, 3, tracker.GetTotalDenials())

	tracker.Reset()
	assert.Equal(t, 0, tracker.GetTotalDenials())
	assert.Equal(t, 0, tracker.GetDenialCount("bash"))
	assert.False(t, tracker.ShouldSuggestRule("bash"))
}

func TestDenialTracker_ResetTool(t *testing.T) {
	tracker := NewDenialTracker()

	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	tracker.RecordDenial("write_file")

	tracker.ResetTool("bash")
	assert.Equal(t, 0, tracker.GetDenialCount("bash"))
	assert.Equal(t, 1, tracker.GetDenialCount("write_file"))
	assert.Equal(t, 1, tracker.GetTotalDenials())
}

func TestDenialTracker_ConcurrencySafe(t *testing.T) {
	tracker := NewDenialTracker()
	done := make(chan struct{})

	// Run concurrent denials — should not panic
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				tracker.RecordDenial("bash")
				tracker.GetDenialCount("bash")
				tracker.GetTotalDenials()
				tracker.ShouldSuggestRule("bash")
				tracker.ShouldSuggestStrictMode()
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 1000, tracker.GetTotalDenials())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -run TestDenialTracker`
Expected: FAIL -- DenialTracker doesn't exist yet

- [ ] **Step 3: Write the denial tracker implementation**

```go
// cmd/celeste/permissions/denial.go
package permissions

import "sync"

const (
	// suggestRuleThreshold is the number of consecutive denials of the same
	// tool before suggesting the user add a permanent deny rule.
	suggestRuleThreshold = 3

	// suggestStrictModeThreshold is the total number of denials across all
	// tools before suggesting the user switch to strict mode.
	suggestStrictModeThreshold = 5
)

// DenialTracker tracks tool execution denials within a session.
// It is safe for concurrent use.
type DenialTracker struct {
	mu     sync.Mutex
	counts map[string]int // toolName -> denial count
	total  int
}

// NewDenialTracker creates a new DenialTracker.
func NewDenialTracker() *DenialTracker {
	return &DenialTracker{
		counts: make(map[string]int),
	}
}

// RecordDenial records a denial for the given tool.
func (dt *DenialTracker) RecordDenial(toolName string) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.counts[toolName]++
	dt.total++
}

// GetDenialCount returns the number of denials recorded for a specific tool.
func (dt *DenialTracker) GetDenialCount(toolName string) int {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.counts[toolName]
}

// GetTotalDenials returns the total number of denials across all tools.
func (dt *DenialTracker) GetTotalDenials() int {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.total
}

// ShouldSuggestRule returns true if the user has denied the given tool
// enough times (3+) that the system should suggest adding a permanent
// deny rule to their config.
func (dt *DenialTracker) ShouldSuggestRule(toolName string) bool {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.counts[toolName] >= suggestRuleThreshold
}

// ShouldSuggestStrictMode returns true if the total denials across all
// tools have reached the threshold (5+) where suggesting strict mode
// would be appropriate.
func (dt *DenialTracker) ShouldSuggestStrictMode() bool {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.total >= suggestStrictModeThreshold
}

// Reset clears all denial tracking data.
func (dt *DenialTracker) Reset() {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.counts = make(map[string]int)
	dt.total = 0
}

// ResetTool clears denial tracking data for a specific tool.
func (dt *DenialTracker) ResetTool(toolName string) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if count, ok := dt.counts[toolName]; ok {
		dt.total -= count
		delete(dt.counts, toolName)
	}
}
```

- [ ] **Step 4: Run tests, verify green**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -run TestDenialTracker -race`
Expected: All tests PASS (including race detector)

---

### Task 5: Persistent Config

**Files:**
- Create: `cmd/celeste/permissions/config.go`
- Test: `cmd/celeste/permissions/config_test.go`

- [ ] **Step 1: Write the test for persistent config**

```go
// cmd/celeste/permissions/config_test.go
package permissions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, ModeDefault, cfg.Mode)

	// Should have sudo and su in alwaysDeny
	assert.GreaterOrEqual(t, len(cfg.AlwaysDeny), 2)

	hasSudo := false
	hasSu := false
	for _, rule := range cfg.AlwaysDeny {
		if rule.ToolPattern == "bash(sudo *)" {
			hasSudo = true
		}
		if rule.ToolPattern == "bash(su *)" {
			hasSu = true
		}
	}
	assert.True(t, hasSudo, "default config should deny sudo")
	assert.True(t, hasSu, "default config should deny su")

	// Default alwaysAllow should include read tools
	assert.NotEmpty(t, cfg.AlwaysAllow)
}

func TestSaveAndLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")

	original := PermissionConfig{
		Mode: ModeStrict,
		AlwaysAllow: []Rule{
			{ToolPattern: "read_file", Decision: Allow},
			{ToolPattern: "list_files", Decision: Allow},
		},
		AlwaysDeny: []Rule{
			{ToolPattern: "bash(sudo *)", Decision: Deny},
			{ToolPattern: "bash(rm -rf *)", Decision: Deny},
		},
		PatternRules: []Rule{
			{ToolPattern: "bash(git *)", Decision: Allow},
		},
	}

	// Save
	err := SaveConfig(path, &original)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Load
	loaded, err := LoadConfig(path)
	require.NoError(t, err)

	assert.Equal(t, original.Mode, loaded.Mode)
	assert.Equal(t, len(original.AlwaysAllow), len(loaded.AlwaysAllow))
	assert.Equal(t, len(original.AlwaysDeny), len(loaded.AlwaysDeny))
	assert.Equal(t, len(original.PatternRules), len(loaded.PatternRules))

	for i, rule := range original.AlwaysAllow {
		assert.Equal(t, rule.ToolPattern, loaded.AlwaysAllow[i].ToolPattern)
		assert.Equal(t, rule.Decision, loaded.AlwaysDeny[i].Decision) // Decision not serialized for allow list
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	cfg, err := LoadConfig(path)
	require.NoError(t, err, "missing file should return default config, not error")
	assert.Equal(t, ModeDefault, cfg.Mode)
	assert.NotEmpty(t, cfg.AlwaysDeny, "should have default deny rules")
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	err := os.WriteFile(path, []byte("{invalid json"), 0644)
	require.NoError(t, err)

	_, err = LoadConfig(path)
	assert.Error(t, err)
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	err := os.WriteFile(path, []byte("{}"), 0644)
	require.NoError(t, err)

	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, ModeDefault, cfg.Mode, "empty config should default to ModeDefault")
}

func TestLoadConfig_InvalidMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid_mode.json")
	err := os.WriteFile(path, []byte(`{"mode":"yolo"}`), 0644)
	require.NoError(t, err)

	_, err = LoadConfig(path)
	assert.Error(t, err, "invalid mode should return error")
}

func TestSaveConfig_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "permissions.json")

	err := SaveConfig(path, &PermissionConfig{Mode: ModeDefault})
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestConfigJSON_Format(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")

	cfg := PermissionConfig{
		Mode: ModeDefault,
		AlwaysAllow: []Rule{
			{ToolPattern: "read_file", Decision: Allow},
		},
		AlwaysDeny: []Rule{
			{ToolPattern: "bash(sudo *)", Decision: Deny},
		},
	}

	err := SaveConfig(path, &cfg)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, `"mode"`)
	assert.Contains(t, content, `"default"`)
	assert.Contains(t, content, `"always_allow"`)
	assert.Contains(t, content, `"always_deny"`)
	assert.Contains(t, content, `"read_file"`)
	assert.Contains(t, content, `"bash(sudo *)"`)
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	assert.Contains(t, path, ".celeste")
	assert.Contains(t, path, "permissions.json")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -run TestDefaultConfig`
Expected: FAIL -- config functions don't exist yet

- [ ] **Step 3: Write the config implementation**

```go
// cmd/celeste/permissions/config.go
package permissions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PermissionConfig holds the persistent permission configuration.
// Stored at ~/.celeste/permissions.json.
type PermissionConfig struct {
	// Mode is the default permission mode (default, strict, trust).
	Mode PermissionMode `json:"mode"`

	// AlwaysAllow is a list of rules that are always permitted regardless of mode.
	AlwaysAllow []Rule `json:"always_allow,omitempty"`

	// AlwaysDeny is a list of rules that are always blocked regardless of mode.
	AlwaysDeny []Rule `json:"always_deny,omitempty"`

	// PatternRules are evaluated after alwaysDeny and alwaysAllow but before
	// mode fallthrough. They allow fine-grained control over specific tool/input
	// combinations.
	PatternRules []Rule `json:"pattern_rules,omitempty"`
}

// configJSON is the on-disk JSON representation. We use a separate struct
// to control serialization (e.g., Decision is stored as string, not int).
type configJSON struct {
	Mode         string       `json:"mode"`
	AlwaysAllow  []ruleJSON   `json:"always_allow,omitempty"`
	AlwaysDeny   []ruleJSON   `json:"always_deny,omitempty"`
	PatternRules []ruleJSON   `json:"pattern_rules,omitempty"`
}

type ruleJSON struct {
	ToolPattern  string `json:"tool_pattern"`
	InputPattern string `json:"input_pattern,omitempty"`
	Decision     string `json:"decision"`
}

// DefaultConfig returns a PermissionConfig with sensible defaults:
//   - Mode: default (auto-allow reads, ask for writes)
//   - AlwaysDeny: sudo and su commands
//   - AlwaysAllow: read_file, list_files, search_files
func DefaultConfig() PermissionConfig {
	return PermissionConfig{
		Mode: ModeDefault,
		AlwaysAllow: []Rule{
			{ToolPattern: "read_file", Decision: Allow},
			{ToolPattern: "list_files", Decision: Allow},
			{ToolPattern: "search_files", Decision: Allow},
		},
		AlwaysDeny: []Rule{
			{ToolPattern: "bash(sudo *)", Decision: Deny},
			{ToolPattern: "bash(su *)", Decision: Deny},
		},
	}
}

// DefaultConfigPath returns the default path for the permissions config file.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".celeste", "permissions.json")
}

// LoadConfig reads a PermissionConfig from disk. If the file does not exist,
// it returns DefaultConfig() without error. If the file exists but contains
// invalid JSON or an unrecognized mode, it returns an error.
func LoadConfig(path string) (*PermissionConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := DefaultConfig()
			return &cfg, nil
		}
		return nil, fmt.Errorf("read permissions config: %w", err)
	}

	var raw configJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse permissions config: %w", err)
	}

	// Validate and parse mode
	mode := ModeDefault
	if raw.Mode != "" {
		parsed, err := ParsePermissionMode(raw.Mode)
		if err != nil {
			return nil, fmt.Errorf("permissions config: %w", err)
		}
		mode = parsed
	}

	cfg := &PermissionConfig{
		Mode:         mode,
		AlwaysAllow:  convertRulesFromJSON(raw.AlwaysAllow, Allow),
		AlwaysDeny:   convertRulesFromJSON(raw.AlwaysDeny, Deny),
		PatternRules: convertRulesFromJSON(raw.PatternRules, Ask),
	}

	return cfg, nil
}

// SaveConfig writes a PermissionConfig to disk as formatted JSON.
// Parent directories are created if they don't exist.
func SaveConfig(path string, config *PermissionConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	raw := configJSON{
		Mode:         config.Mode.String(),
		AlwaysAllow:  convertRulesToJSON(config.AlwaysAllow),
		AlwaysDeny:   convertRulesToJSON(config.AlwaysDeny),
		PatternRules: convertRulesToJSON(config.PatternRules),
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal permissions config: %w", err)
	}

	// Append newline for POSIX compliance
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write permissions config: %w", err)
	}

	return nil
}

// convertRulesFromJSON converts JSON rule representations to Rule structs.
// defaultDecision is used when the rule's decision field is empty.
func convertRulesFromJSON(jsonRules []ruleJSON, defaultDecision Decision) []Rule {
	if len(jsonRules) == 0 {
		return nil
	}

	rules := make([]Rule, len(jsonRules))
	for i, jr := range jsonRules {
		decision := defaultDecision
		switch jr.Decision {
		case "allow":
			decision = Allow
		case "deny":
			decision = Deny
		case "ask":
			decision = Ask
		}

		rules[i] = Rule{
			ToolPattern:  jr.ToolPattern,
			InputPattern: jr.InputPattern,
			Decision:     decision,
		}
	}
	return rules
}

// convertRulesToJSON converts Rule structs to JSON representations.
func convertRulesToJSON(rules []Rule) []ruleJSON {
	if len(rules) == 0 {
		return nil
	}

	jsonRules := make([]ruleJSON, len(rules))
	for i, r := range rules {
		jsonRules[i] = ruleJSON{
			ToolPattern:  r.ToolPattern,
			InputPattern: r.InputPattern,
			Decision:     r.Decision.String(),
		}
	}
	return jsonRules
}
```

- [ ] **Step 4: Run tests, verify green**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -run "TestDefaultConfig|TestSaveAndLoad|TestLoadConfig|TestConfigJSON|TestDefaultConfigPath"`
Expected: All tests PASS

---

### Task 6: Integration with Tool Executor

**Files:**
- Modify: `cmd/celeste/tools/registry.go` (from Plan 1)
- No new tests in this task (integration tests are in Task 8)

This task describes how to wire the permission system into the existing tool execution path. Plan 1 establishes a `Registry` with a tool execution flow. This task adds a permission check gate.

- [ ] **Step 1: Add ToolInfo adapter to the tools package**

The `permissions.ToolInfo` interface requires `ToolName() string` and `IsReadOnly() bool`. The `tools.Tool` interface from Plan 1 has `Name() string` and `IsReadOnly() bool`. We need a thin adapter since the method name differs (`Name` vs `ToolName`).

Add to `cmd/celeste/tools/registry.go`:

```go
import "github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"

// toolInfoAdapter wraps a Tool to satisfy permissions.ToolInfo.
type toolInfoAdapter struct {
	tool Tool
}

func (a *toolInfoAdapter) ToolName() string { return a.tool.Name() }
func (a *toolInfoAdapter) IsReadOnly() bool { return a.tool.IsReadOnly() }
```

- [ ] **Step 2: Add permission checker field and setter to Registry**

Add to the `Registry` struct in `cmd/celeste/tools/registry.go`:

```go
type Registry struct {
	// ... existing fields from Plan 1 ...
	tools   map[string]Tool
	mu      sync.RWMutex

	// permChecker is the optional permission checker. When set, every tool
	// execution passes through it before running.
	permChecker *permissions.Checker

	// denialTracker tracks user denials for suggesting rules.
	denialTracker *permissions.DenialTracker
}

// SetPermissionChecker sets the permission checker for all tool executions.
// When set, Check() is called before every Execute(). If the result is Deny,
// Execute returns an error. If the result is Ask, Execute returns a special
// ToolResult with PermissionRequired: true.
func (r *Registry) SetPermissionChecker(checker *permissions.Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permChecker = checker
	r.denialTracker = permissions.NewDenialTracker()
}

// DenialTracker returns the denial tracker, if a permission checker is set.
func (r *Registry) DenialTracker() *permissions.DenialTracker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.denialTracker
}
```

- [ ] **Step 3: Add permission check to the execution path**

Modify the `Execute` method (or equivalent from Plan 1/2) to check permissions:

```go
// ExecuteResult extends ToolResult with permission status.
type ExecuteResult struct {
	ToolResult

	// PermissionRequired is true when the user must approve this execution.
	// The TUI should render a prompt and retry with ForceAllow if approved.
	PermissionRequired bool

	// PermissionReason explains why permission is needed or was denied.
	PermissionReason string
}

// Execute runs a tool by name with the given input, checking permissions first.
func (r *Registry) Execute(ctx context.Context, name string, input map[string]any, progress chan<- ProgressEvent) (ExecuteResult, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	checker := r.permChecker
	tracker := r.denialTracker
	r.mu.RUnlock()

	if !ok {
		return ExecuteResult{}, fmt.Errorf("unknown tool: %s", name)
	}

	// Permission check
	if checker != nil {
		info := &toolInfoAdapter{tool: tool}
		result := checker.Check(info, input)

		switch result.Decision {
		case permissions.Deny:
			if tracker != nil {
				tracker.RecordDenial(name)
			}
			return ExecuteResult{
				ToolResult:       ToolResult{Content: "Permission denied: " + result.Reason, Error: true},
				PermissionReason: result.Reason,
			}, nil

		case permissions.Ask:
			return ExecuteResult{
				PermissionRequired: true,
				PermissionReason:   result.Reason,
			}, nil

		case permissions.Allow:
			// Fall through to execution
		}
	}

	// Execute the tool
	toolResult, err := tool.Execute(ctx, input, progress)
	if err != nil {
		return ExecuteResult{ToolResult: toolResult}, err
	}

	return ExecuteResult{ToolResult: toolResult}, nil
}

// ExecuteWithOverride runs a tool bypassing the permission check.
// Used when the user has explicitly approved the execution via a TUI prompt.
func (r *Registry) ExecuteWithOverride(ctx context.Context, name string, input map[string]any, progress chan<- ProgressEvent) (ExecuteResult, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()

	if !ok {
		return ExecuteResult{}, fmt.Errorf("unknown tool: %s", name)
	}

	toolResult, err := tool.Execute(ctx, input, progress)
	if err != nil {
		return ExecuteResult{ToolResult: toolResult}, err
	}

	return ExecuteResult{ToolResult: toolResult}, nil
}
```

- [ ] **Step 4: Verify compilation**

Run: `cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/tools/...`
Expected: Compiles without errors

---

### Task 7: Wire into main.go

**Files:**
- Modify: `cmd/celeste/main.go`

This task loads the permission config at startup and wires the checker into the tool registry.

- [ ] **Step 1: Add imports and init logic to main.go**

Add to the startup sequence in `cmd/celeste/main.go` (the exact location depends on Plan 1's structure -- place it after the `tools.Registry` is created):

```go
import (
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
)

// In the startup function, after registry creation:

// Load permission configuration
permCfgPath := permissions.DefaultConfigPath()
permCfg, err := permissions.LoadConfig(permCfgPath)
if err != nil {
	// Log warning but don't fail startup -- use defaults
	fmt.Fprintf(os.Stderr, "Warning: failed to load permissions config from %s: %v (using defaults)\n", permCfgPath, err)
	defaultCfg := permissions.DefaultConfig()
	permCfg = &defaultCfg
}

// Create checker and wire into registry
checker := permissions.NewChecker(*permCfg)
registry.SetPermissionChecker(checker)
```

- [ ] **Step 2: Handle --trust flag (optional CLI override)**

If a `--trust` or `--permission-mode` flag exists (or should be added), override the config mode:

```go
// After loading config, before creating checker:
if trustFlag {
	permCfg.Mode = permissions.ModeTrust
}
// Or:
if permissionModeFlag != "" {
	mode, err := permissions.ParsePermissionMode(permissionModeFlag)
	if err != nil {
		return fmt.Errorf("invalid --permission-mode: %w", err)
	}
	permCfg.Mode = mode
}
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/`
Expected: Compiles without errors

---

### Task 8: Final Verification

- [ ] **Step 1: Run all permission tests**

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -race -count=1
```

Expected: All tests PASS, no race conditions

- [ ] **Step 2: Run all project tests**

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./... -race -count=1 -timeout 120s
```

Expected: All tests PASS (including existing tests unaffected by changes)

- [ ] **Step 3: Verify the build**

```bash
cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/
```

Expected: Clean build

- [ ] **Step 4: Verify config file format**

Create a test config and verify it round-trips correctly:

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/permissions/ -v -run TestConfigJSON_Format
```

- [ ] **Step 5: Manual smoke test**

Run celeste-cli and verify:
1. Without `~/.celeste/permissions.json` -- defaults apply, sudo is blocked
2. With `{"mode":"trust"}` -- all tools auto-allowed
3. With `{"mode":"strict"}` -- all tools require prompt

- [ ] **Step 6: Verify no circular dependencies**

```bash
cd /Users/kusanagi/Development/celeste-cli && go vet ./cmd/celeste/permissions/ ./cmd/celeste/tools/...
```

Expected: No errors. The `permissions` package depends only on the standard library. The `tools` package imports `permissions`, not the other way around.

---

## Summary

| Task | Files | Description |
|------|-------|-------------|
| 1 | `permissions/permission.go`, `*_test.go` | Core types: Decision, PermissionMode, Rule, CheckResult |
| 2 | `permissions/rules.go`, `*_test.go` | Pattern matching: MatchRule, ParseToolPattern, glob matching |
| 3 | `permissions/checker.go`, `*_test.go` | 5-step evaluation chain: Checker.Check() |
| 4 | `permissions/denial.go`, `*_test.go` | Denial tracking with rule/mode suggestion thresholds |
| 5 | `permissions/config.go`, `*_test.go` | Persistent JSON config with LoadConfig/SaveConfig |
| 6 | `tools/registry.go` (modified) | Permission gate in tool execution path |
| 7 | `main.go` (modified) | Startup wiring: load config, create checker, set on registry |
| 8 | (verification) | Full test suite, build, smoke test |

**Dependency graph:** Task 1 -> Task 2 -> Task 3 -> Task 4 (independent) -> Task 5 (independent) -> Task 6 -> Task 7 -> Task 8

Tasks 4 and 5 can be implemented in parallel after Task 3 is complete. Task 6 depends on Tasks 3 and 5. Task 7 depends on Task 6.
