package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// SpawnAgentTool is a built-in tool that allows the model to spawn subagents
// for task delegation. The subagent runs in the foreground (blocks until
// complete) and returns its final result.
type SpawnAgentTool struct {
	manager *Manager
}

// NewSpawnAgentTool creates a new spawn_agent tool backed by the given manager.
func NewSpawnAgentTool(manager *Manager) *SpawnAgentTool {
	return &SpawnAgentTool{manager: manager}
}

func (t *SpawnAgentTool) Name() string { return "spawn_agent" }

func (t *SpawnAgentTool) Description() string {
	return "Spawn a subagent to handle a subtask. The subagent has the same tools, permissions, and persona as you. It runs to completion and returns its result. Use this for independent subtasks that benefit from a fresh context."
}

func (t *SpawnAgentTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"goal": {
				"type": "string",
				"description": "A clear, self-contained description of what the subagent should accomplish"
			},
			"workspace": {
				"type": "string",
				"description": "Working directory for the subagent (defaults to current workspace)"
			}
		},
		"required": ["goal"]
	}`)
}

func (t *SpawnAgentTool) IsConcurrencySafe(input map[string]any) bool { return false }
func (t *SpawnAgentTool) IsReadOnly() bool                            { return false }

func (t *SpawnAgentTool) InterruptBehavior() tools.InterruptBehavior {
	return tools.InterruptCancel
}

func (t *SpawnAgentTool) ValidateInput(input map[string]any) error {
	goal, ok := input["goal"].(string)
	if !ok || goal == "" {
		return fmt.Errorf("'goal' is required and must be a non-empty string")
	}
	return nil
}

func (t *SpawnAgentTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	goal := input["goal"].(string)
	workspace, _ := input["workspace"].(string)

	// Emit progress so the TUI shows what is happening
	if progress != nil {
		progress <- tools.ProgressEvent{
			ToolName: "spawn_agent",
			Message:  fmt.Sprintf("Spawning subagent: %s", truncate(goal, 80)),
		}
	}

	run, err := t.manager.Spawn(ctx, goal, workspace)
	if err != nil {
		return tools.ToolResult{
			Content: fmt.Sprintf("Subagent failed: %v", err),
			Error:   true,
		}, nil
	}

	// Format result with metadata
	result := fmt.Sprintf("Subagent %s completed in %d turns (%s)\n\n%s",
		run.ID, run.Turns, run.EndedAt.Sub(run.StartedAt).Round(time.Millisecond), run.Result)

	return tools.ToolResult{
		Content: result,
		Metadata: map[string]any{
			"subagent_id": run.ID,
			"turns":       run.Turns,
			"status":      run.Status,
		},
	}, nil
}

// truncate shortens s to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
