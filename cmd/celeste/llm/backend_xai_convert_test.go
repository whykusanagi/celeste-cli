package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func TestXAIConvertToolsSuccess(t *testing.T) {
	backend := &XAIBackend{}

	tools := backend.convertTools([]tui.SkillDefinition{
		{
			Name:        "echo",
			Description: "Echo text",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	})

	require.Len(t, tools, 1)
	assert.Equal(t, "function", tools[0].Type)
	assert.Equal(t, "echo", tools[0].Function.Name)
}

func TestXAIConvertToolsSkipsMarshalErrors(t *testing.T) {
	backend := &XAIBackend{}

	tools := backend.convertTools([]tui.SkillDefinition{
		{
			Name:        "valid_tool",
			Description: "Valid",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "bad_tool",
			Description: "Invalid params",
			Parameters: map[string]any{
				"bad": func() {},
			},
		},
	})

	require.Len(t, tools, 1)
	assert.Equal(t, "valid_tool", tools[0].Function.Name)
}
