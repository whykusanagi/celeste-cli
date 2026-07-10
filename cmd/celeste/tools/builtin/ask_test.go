package builtin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestAskTool_HeadlessReturnsError(t *testing.T) {
	reg := tools.NewRegistry() // no askFn installed
	tool := NewAskTool(reg)
	res, err := tool.Execute(context.Background(), map[string]any{
		"question": "pick",
		"options":  []any{map[string]any{"label": "a"}},
	}, nil)
	require.NoError(t, err) // tool-level error surfaces via ToolResult, not err
	assert.True(t, res.Error)
	assert.Contains(t, res.Content, "unavailable")
}

func TestAskTool_ReturnsSelection(t *testing.T) {
	reg := tools.NewRegistry()
	reg.SetAskFunc(func(ctx context.Context, req tools.AskRequest) (tools.AskResponse, error) {
		require.Len(t, req.Options, 2)
		return tools.AskResponse{Selected: []string{"blue"}}, nil
	})
	tool := NewAskTool(reg)
	res, err := tool.Execute(context.Background(), map[string]any{
		"question": "color?",
		"options": []any{
			map[string]any{"label": "red"},
			map[string]any{"label": "blue"},
		},
	}, nil)
	require.NoError(t, err)
	assert.False(t, res.Error)
	assert.Equal(t, "blue", res.Content)
}

func TestAskTool_Cancelled(t *testing.T) {
	reg := tools.NewRegistry()
	reg.SetAskFunc(func(ctx context.Context, req tools.AskRequest) (tools.AskResponse, error) {
		return tools.AskResponse{Cancelled: true}, nil
	})
	res, _ := NewAskTool(reg).Execute(context.Background(), map[string]any{
		"question": "q", "options": []any{map[string]any{"label": "x"}},
	}, nil)
	assert.Contains(t, res.Content, "cancelled")
}
