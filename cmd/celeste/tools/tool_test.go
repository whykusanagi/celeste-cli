package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTool struct {
	name              string
	description       string
	params            json.RawMessage
	concurrencySafe   bool
	readOnly          bool
	interruptBehavior InterruptBehavior
	executeFunc       func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error)
}

func (m *mockTool) Name() string                                { return m.name }
func (m *mockTool) Description() string                         { return m.description }
func (m *mockTool) Parameters() json.RawMessage                 { return m.params }
func (m *mockTool) IsConcurrencySafe(input map[string]any) bool { return m.concurrencySafe }
func (m *mockTool) IsReadOnly() bool                            { return m.readOnly }
func (m *mockTool) ValidateInput(input map[string]any) error    { return nil }
func (m *mockTool) InterruptBehavior() InterruptBehavior        { return m.interruptBehavior }
func (m *mockTool) Execute(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, input, progress)
	}
	return ToolResult{Content: "ok"}, nil
}

func TestToolResult(t *testing.T) {
	r := ToolResult{Content: "hello", Error: false, Metadata: map[string]any{"key": "val"}}
	assert.Equal(t, "hello", r.Content)
	assert.False(t, r.Error)
	assert.Equal(t, "val", r.Metadata["key"])
}

func TestProgressEvent(t *testing.T) {
	p := ProgressEvent{ToolName: "bash", Message: "running", Percent: 0.5}
	assert.Equal(t, "bash", p.ToolName)
	assert.Equal(t, 0.5, p.Percent)
}

func TestInterruptBehavior(t *testing.T) {
	assert.Equal(t, InterruptBehavior(0), InterruptCancel)
	assert.Equal(t, InterruptBehavior(1), InterruptBlock)
}

func TestMockToolImplementsInterface(t *testing.T) {
	var _ Tool = &mockTool{}
	tool := &mockTool{
		name:        "test",
		description: "test tool",
		params:      json.RawMessage(`{"type":"object","properties":{}}`),
		readOnly:    true,
	}
	assert.Equal(t, "test", tool.Name())
	assert.True(t, tool.IsReadOnly())
	result, err := tool.Execute(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Content)
}
