package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
