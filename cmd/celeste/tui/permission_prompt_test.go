package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
)

// TestBuildPattern_MatchesViaMatchRule verifies that the pattern returned by
// buildPattern() is a matchable ToolPattern: after AddPersistentAllow stores
// it as a rule, a subsequent call with DIFFERENT args must be matched by
// MatchRule, proving the "always allow" semantic generalises.
func TestBuildPattern_MatchesViaMatchRule(t *testing.T) {
	// Simulate the model state after the user sees the initial prompt for "bash"
	// with inputSummary "echo hello".
	m := PermissionPromptModel{
		toolName:     "bash",
		inputSummary: "echo hello",
	}
	pattern := m.buildPattern()

	// Pattern must be the bare tool name, not a truncated display string.
	assert.Equal(t, "bash", pattern, "buildPattern must return bare tool name")

	// Build a Rule as the registry would when persisting an always-allow.
	rule := permissions.Rule{
		ToolPattern: pattern,
		Decision:    permissions.Allow,
	}

	// First call: same args as the original prompt — must match.
	assert.True(t,
		permissions.MatchRule(rule, "bash", map[string]any{"command": "echo hello"}),
		"pattern must match the original invocation",
	)

	// Second call: DIFFERENT args — the always-allow must still fire.
	assert.True(t,
		permissions.MatchRule(rule, "bash", map[string]any{"command": "git status"}),
		"pattern must match a subsequent invocation with different args",
	)

	// Sanity: a different tool must not match.
	assert.False(t,
		permissions.MatchRule(rule, "write_file", map[string]any{"path": "/tmp/foo"}),
		"pattern must not match a different tool",
	)
}

// TestBuildPattern_TruncatedDisplayDoesNotBreakPattern ensures that even when
// inputSummary contains the ellipsis added by the display truncation logic,
// buildPattern still returns a matchable bare tool name.
func TestBuildPattern_TruncatedDisplayDoesNotBreakPattern(t *testing.T) {
	// Simulate a very long command that would be truncated in the display.
	m := PermissionPromptModel{
		toolName:     "bash",
		inputSummary: "rm -rf /very/long/path/that/gets/truncated/by/the/UI...",
	}
	pattern := m.buildPattern()

	// Despite the truncated display string, pattern is still the bare tool name.
	assert.Equal(t, "bash", pattern)

	rule := permissions.Rule{ToolPattern: pattern, Decision: permissions.Allow}

	// A real follow-up call with a completely different command must match.
	assert.True(t,
		permissions.MatchRule(rule, "bash", map[string]any{"command": "ls -la"}),
		"always-allow rule must fire on any subsequent bash invocation",
	)
}
