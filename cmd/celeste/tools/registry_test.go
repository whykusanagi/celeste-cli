package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	tool := &mockTool{name: "test", description: "a test"}
	r.Register(tool)
	got, ok := r.Get("test")
	require.True(t, ok)
	assert.Equal(t, "test", got.Name())
}

func TestRegistryGetMissing(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	assert.False(t, ok)
}

func TestRegistryGetAll(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "b"})
	r.Register(&mockTool{name: "a"})
	all := r.GetAll()
	require.Len(t, all, 2)
	assert.Equal(t, "a", all[0].Name())
	assert.Equal(t, "b", all[1].Name())
}

func TestRegistryGetToolDefinitions(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{
		name:        "weather",
		description: "get weather",
		params:      json.RawMessage(`{"type":"object","properties":{"zip":{"type":"string"}}}`),
	})
	defs := r.GetToolDefinitions()
	require.Len(t, defs, 1)
	assert.Equal(t, "function", defs[0]["type"])
	fn := defs[0]["function"].(map[string]any)
	assert.Equal(t, "weather", fn["name"])
}

func TestRegistryGetTools_ModeFiltering(t *testing.T) {
	r := NewRegistry()
	r.RegisterWithModes(&mockTool{name: "bash"}, ModeAgent, ModeClaw, ModeChat)
	r.RegisterWithModes(&mockTool{name: "tarot"}, ModeChat, ModeClaw)
	agentTools := r.GetTools(ModeAgent)
	assert.Len(t, agentTools, 1)
	assert.Equal(t, "bash", agentTools[0].Name())
	chatTools := r.GetTools(ModeChat)
	assert.Len(t, chatTools, 2)
}

func TestRegistryExecute(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{
		name: "echo",
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			return ToolResult{Content: input["text"].(string)}, nil
		},
	})
	result, err := r.Execute(context.Background(), "echo", map[string]any{"text": "hello"})
	require.NoError(t, err)
	assert.Equal(t, "hello", result.Content)
}

func TestRegistryExecuteMissing(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "nope", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRegistryCount(t *testing.T) {
	r := NewRegistry()
	assert.Equal(t, 0, r.Count())
	r.Register(&mockTool{name: "a"})
	assert.Equal(t, 1, r.Count())
}

func TestRegistryExecutePermissionDenied(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "dangerous_tool"})

	// Create a checker that denies "dangerous_tool"
	cfg := permissions.PermissionConfig{
		Mode: permissions.ModeDefault,
		AlwaysDeny: []permissions.Rule{
			{ToolPattern: "dangerous_tool", Decision: permissions.Deny},
		},
	}
	checker := permissions.NewChecker(cfg)
	r.SetPermissionChecker(checker)

	result, err := r.Execute(context.Background(), "dangerous_tool", map[string]any{})
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "Permission denied")
}

func TestRegistryExecutePermissionAllowed(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{
		name:     "safe_tool",
		readOnly: true,
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			return ToolResult{Content: "executed"}, nil
		},
	})

	// Create a checker in default mode — read-only tools should be auto-allowed
	cfg := permissions.PermissionConfig{Mode: permissions.ModeDefault}
	checker := permissions.NewChecker(cfg)
	r.SetPermissionChecker(checker)

	result, err := r.Execute(context.Background(), "safe_tool", map[string]any{})
	require.NoError(t, err)
	assert.False(t, result.Error)
	assert.Equal(t, "executed", result.Content)
}

func TestRegistryExecuteNoChecker(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{
		name: "any_tool",
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			return ToolResult{Content: "ran"}, nil
		},
	})

	// No checker set — should allow all
	result, err := r.Execute(context.Background(), "any_tool", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "ran", result.Content)
}
