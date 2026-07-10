package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// AskTool lets the model ask the user a structured multiple-choice question
// mid-turn. It blocks until the user answers (interactive TUI) or returns an
// error result in non-interactive contexts.
type AskTool struct {
	BaseTool
	registry *tools.Registry
}

// NewAskTool creates the ask tool bound to the registry that carries the
// interactive callback.
func NewAskTool(registry *tools.Registry) *AskTool {
	return &AskTool{
		BaseTool: BaseTool{
			ToolName:        "ask",
			ToolDescription: "Ask the user a structured multiple-choice question and wait for their answer. Use when you need a decision only the user can make. Provide 2-4 clear options.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"question": {"type": "string", "description": "The question to ask."},
					"options": {
						"type": "array",
						"description": "The choices to present.",
						"items": {
							"type": "object",
							"properties": {
								"label": {"type": "string"},
								"description": {"type": "string"}
							},
							"required": ["label"]
						}
					},
					"multi_select": {"type": "boolean", "description": "Allow selecting multiple options."}
				},
				"required": ["question", "options"]
			}`),
			ReadOnly:        true,
			ConcurrencySafe: false,
			RequiredFields:  []string{"question", "options"},
		},
		registry: registry,
	}
}

func (t *AskTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	question, _ := input["question"].(string)
	multi, _ := input["multi_select"].(bool)

	var options []tools.AskOption
	if raw, ok := input["options"].([]any); ok {
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			label, _ := m["label"].(string)
			desc, _ := m["description"].(string)
			if label != "" {
				options = append(options, tools.AskOption{Label: label, Description: desc})
			}
		}
	}
	if question == "" || len(options) == 0 {
		return tools.ToolResult{Content: "ask requires a question and at least one option", Error: true}, nil
	}

	resp, err := t.registry.Ask(ctx, tools.AskRequest{
		Question:    question,
		Options:     options,
		MultiSelect: multi,
	})
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("interactive input unavailable in this context: %v", err), Error: true}, nil
	}
	if resp.Cancelled {
		return tools.ToolResult{Content: "The user cancelled the question without selecting an option."}, nil
	}
	return tools.ToolResult{Content: strings.Join(resp.Selected, ", ")}, nil
}
