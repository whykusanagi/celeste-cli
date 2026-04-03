package hooks

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook executor uses sh -c which is not available on Windows")
	}
}

func TestExecutor_PreToolUse_Approve(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PreToolUse", Tool: "*", Command: "true", Timeout: 5},
	}
	exec := NewExecutor(hooks, t.TempDir())
	result, err := exec.RunPreToolUse("bash", nil)
	require.NoError(t, err)
	assert.Equal(t, "approve", result.Decision)
	assert.Equal(t, 0, result.ExitCode)
}

func TestExecutor_PreToolUse_Block(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PreToolUse", Tool: "*", Command: "echo blocked && exit 1", Timeout: 5},
	}
	exec := NewExecutor(hooks, t.TempDir())
	result, err := exec.RunPreToolUse("bash", nil)
	require.NoError(t, err)
	assert.Equal(t, "block", result.Decision)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, "blocked", result.Output)
}

func TestExecutor_ToolFiltering(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PreToolUse", Tool: "write_file", Command: "exit 1", Timeout: 5},
	}
	exec := NewExecutor(hooks, t.TempDir())

	// Should not match "bash"
	result, err := exec.RunPreToolUse("bash", nil)
	require.NoError(t, err)
	assert.Equal(t, "approve", result.Decision)

	// Should match "write_file"
	result, err = exec.RunPreToolUse("write_file", nil)
	require.NoError(t, err)
	assert.Equal(t, "block", result.Decision)
}

func TestExecutor_PostToolUse(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PostToolUse", Tool: "*", Command: "echo done", Timeout: 5},
	}
	exec := NewExecutor(hooks, t.TempDir())
	result, err := exec.RunPostToolUse("bash", nil)
	require.NoError(t, err)
	assert.Equal(t, "approve", result.Decision)
	assert.Equal(t, "done", result.Output)
}

func TestExecutor_TemplateVars(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PreToolUse", Tool: "*", Command: "echo {{tool}} {{path}}", Timeout: 5},
	}
	workspace := t.TempDir()
	exec := NewExecutor(hooks, workspace)
	result, err := exec.RunPreToolUse("write_file", map[string]any{"path": "/tmp/test.txt"})
	require.NoError(t, err)
	assert.Equal(t, "approve", result.Decision)
	assert.Equal(t, "write_file /tmp/test.txt", result.Output)
}

func TestExpandTemplateVars(t *testing.T) {
	result := expandTemplateVars(
		"cd {{workspace}} && {{tool}} {{path}} {{command}}",
		"/home/user/project",
		"bash",
		map[string]any{"path": "src/main.go", "command": "go build"},
	)
	assert.Equal(t, "cd /home/user/project && bash src/main.go go build", result)
}

func TestExpandTemplateVars_MissingInput(t *testing.T) {
	result := expandTemplateVars("echo {{path}} {{command}}", "/ws", "test", nil)
	assert.Equal(t, "echo  ", result)
}

func TestExecutor_NoMatchingHooks(t *testing.T) {
	skipOnWindows(t)
	hooks := []Hook{
		{Event: "PostToolUse", Tool: "bash", Command: "echo post", Timeout: 5},
	}
	exec := NewExecutor(hooks, t.TempDir())
	// PreToolUse should not match PostToolUse hooks
	result, err := exec.RunPreToolUse("bash", nil)
	require.NoError(t, err)
	assert.Equal(t, "approve", result.Decision)
}
