package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func TestOpenAIConvertToolsSuccess(t *testing.T) {
	backend := NewOpenAIBackend(&Config{})

	tools := backend.convertTools([]tui.SkillDefinition{
		{
			Name:        "echo",
			Description: "Echo text",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
			},
		},
	})

	require.Len(t, tools, 1)
	require.NotNil(t, tools[0].Function)
	assert.Equal(t, "echo", tools[0].Function.Name)
}

func TestOpenAIConvertToolsSkipsMarshalErrors(t *testing.T) {
	backend := NewOpenAIBackend(&Config{})

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

	require.Len(t, tools, 1, "invalid tool should be skipped")
	require.NotNil(t, tools[0].Function)
	assert.Equal(t, "valid_tool", tools[0].Function.Name)
}
