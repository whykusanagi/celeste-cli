package main

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// hookRunnerAdapter bridges the 1.x hooks.Executor to tools.HookRunner.
// These tests lock in the exact 1.x message text at the adapter's own
// boundary (its Reason field): a hook's own "block" decision keeps the
// hook's Output as Reason -- the registry adds the shared "Blocked by
// pre-tool hook: " framing every HookRunner gets (Task 6, approved); a hook
// EXECUTION error (the process itself couldn't run) is reported as bare
// "Hook error: <err>", matching 1.x's registry verbatim before Task 6
// unified every hook runner behind one interface. v1 hooks run via "sh -c"
// and don't exist on Windows.
func TestHookRunnerAdapterPreToolUseBlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("v1 hook execution uses sh -c, not available on windows")
	}
	workspace := t.TempDir()
	executor := hooks.NewExecutor([]hooks.Hook{
		{Event: "PreToolUse", Tool: "*", Command: "printf nope; exit 1"},
	}, workspace)
	a := &hookRunnerAdapter{executor: executor}

	got := a.PreToolUse(context.Background(), "write_file", map[string]any{"path": "x"})
	assert.Equal(t, "deny", got.Decision)
	assert.Equal(t, "nope", got.Reason)
}

func TestHookRunnerAdapterPreToolUseExecutionError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("v1 hook execution uses sh -c, not available on windows")
	}
	// A workspace directory that doesn't exist makes the hook PROCESS ITSELF
	// fail to start (a chdir failure) -- a Go error distinct from the hook
	// choosing to block. 1.x showed this as bare "Hook error: ...".
	badWorkspace := filepath.Join(t.TempDir(), "does-not-exist")
	executor := hooks.NewExecutor([]hooks.Hook{
		{Event: "PreToolUse", Tool: "*", Command: "true"},
	}, badWorkspace)
	a := &hookRunnerAdapter{executor: executor}

	got := a.PreToolUse(context.Background(), "write_file", map[string]any{"path": "x"})
	assert.Equal(t, "deny", got.Decision)
	require.Contains(t, got.Reason, "Hook error:")
}

func TestHookRunnerAdapterPostToolUseExecutionErrorIsLoggedNotFatal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("v1 hook execution uses sh -c, not available on windows")
	}
	badWorkspace := filepath.Join(t.TempDir(), "does-not-exist")
	executor := hooks.NewExecutor([]hooks.Hook{
		{Event: "PostToolUse", Tool: "*", Command: "true"},
	}, badWorkspace)
	a := &hookRunnerAdapter{executor: executor}

	// 1.x logged post-hook errors and moved on; it never blocked or altered
	// the result the model already has.
	got := a.PostToolUse(context.Background(), "write_file", map[string]any{"path": "x"}, tools.ToolResult{Content: "ok"})
	assert.Equal(t, "", got)
}
