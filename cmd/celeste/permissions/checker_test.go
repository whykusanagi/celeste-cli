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

func (t *toolStub) ToolName() string { return t.name }
func (t *toolStub) IsReadOnly() bool { return t.readOnly }

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
