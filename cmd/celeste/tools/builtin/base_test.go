package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestBaseTool(t *testing.T) {
	bt := &BaseTool{
		ToolName:        "test_tool",
		ToolDescription: "A test tool",
		ToolParameters:  json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`),
		ReadOnly:        true,
		ConcurrencySafe: true,
	}
	assert.Equal(t, "test_tool", bt.Name())
	assert.Equal(t, "A test tool", bt.Description())
	assert.True(t, bt.IsReadOnly())
	assert.True(t, bt.IsConcurrencySafe(nil))
	assert.Equal(t, tools.InterruptCancel, bt.InterruptBehavior())
	require.NoError(t, bt.ValidateInput(nil))
}

func TestBaseToolRequiredFields(t *testing.T) {
	bt := &BaseTool{
		ToolName:        "req_test",
		ToolDescription: "Test required",
		ToolParameters:  json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		RequiredFields:  []string{"name"},
	}
	err := bt.ValidateInput(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name")
	err = bt.ValidateInput(map[string]any{"name": "alice"})
	assert.NoError(t, err)
}

func TestBaseToolExecuteNotImplemented(t *testing.T) {
	bt := &BaseTool{ToolName: "noop"}
	_, err := bt.Execute(context.Background(), nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}
