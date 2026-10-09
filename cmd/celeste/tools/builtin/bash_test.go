package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

func TestBashToolName(t *testing.T) {
	bt := NewBashTool(t.TempDir(), nil)
	assert.Equal(t, "bash", bt.Name())
}

func TestBashToolProperties(t *testing.T) {
	bt := NewBashTool(t.TempDir(), nil)
	assert.False(t, bt.IsReadOnly())
	assert.False(t, bt.IsConcurrencySafe(nil))
	assert.Equal(t, tools.InterruptCancel, bt.InterruptBehavior())
}

func TestBashToolExecuteSimpleCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash tool uses sh -c which is not available on Windows")
	}
	bt := NewBashTool(t.TempDir(), nil)
	result, err := bt.Execute(context.Background(), map[string]any{
		"command": "echo hello",
	}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &data))
	assert.Contains(t, data["output"], "hello")
	assert.Equal(t, float64(0), data["exit_code"])
}

func TestBashToolSudoBlocking(t *testing.T) {
	bt := NewBashTool(t.TempDir(), nil)
	result, err := bt.Execute(context.Background(), map[string]any{
		"command": "sudo rm -rf /",
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "privilege escalation")
}

func TestBashToolSuBlocking(t *testing.T) {
	bt := NewBashTool(t.TempDir(), nil)
	result, err := bt.Execute(context.Background(), map[string]any{
		"command": "su root",
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "privilege escalation")
}

func TestBashToolRequiredFieldValidation(t *testing.T) {
	bt := NewBashTool(t.TempDir(), nil)
	result, err := bt.Execute(context.Background(), map[string]any{}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "command")
}

func TestBashToolTimeoutAndExitStatusKeepTheResultKeys(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash tool uses sh -c which is not available on Windows")
	}
	bt := NewBashTool(t.TempDir(), nil)
	result, err := bt.Execute(context.Background(), map[string]any{"command": "sleep 5", "timeout_seconds": 1}, nil)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &data))
	assert.Equal(t, true, data["timed_out"])
	assert.Contains(t, data["error"], "timed out after 1s")

	result, err = bt.Execute(context.Background(), map[string]any{"command": "exit 4"}, nil)
	require.NoError(t, err)
	data = nil
	require.NoError(t, json.Unmarshal([]byte(result.Content), &data))
	assert.Equal(t, float64(4), data["exit_code"])
	assert.Equal(t, "exit status 4", data["error"])
	for _, k := range []string{"command", "workspace", "output", "truncated", "timed_out"} {
		assert.Contains(t, data, k)
	}
}
