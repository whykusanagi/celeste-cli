package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
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
	return "Spawn a subagent to handle a subtask. The subagent runs to completion and returns its result. Use this for independent subtasks that benefit from a fresh context. You can override the subagent's personality via the persona parameter — useful for creating content in different voices (e.g., 'write this in a warm, theatrical style' vs 'write this in a cold, clipped operator style')."
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
			},
			"persona": {
				"type": "object",
				"description": "Override the subagent's personality sliders. Omit to inherit the parent's current sliders.",
				"properties": {
					"preset": {
						"type": "string",
						"description": "Load a named persona preset (from /persona saved presets)"
					},
					"flirt": {
						"type": "integer",
						"description": "Flirt level 0-10 (0=professional, 3=playful, 7=flirty, 10=aggressive)"
					},
					"warmth": {
						"type": "integer",
						"description": "Warmth level 0-10 (0=cold, 3=polite, 7=warm, 10=affectionate)"
					},
					"register": {
						"type": "integer",
						"description": "Speech style 0-10 (0=operator, 3=standard, 7=theatrical, 10=uwu)"
					},
					"lewdness": {
						"type": "integer",
						"description": "Content level 0-10 (requires r18=true to have effect)"
					},
					"r18": {
						"type": "boolean",
						"description": "Enable R18 content eligibility for this subagent"
					}
				}
			}
		},
		"required": ["goal"]
	}`)
}

func (t *SpawnAgentTool) IsConcurrencySafe(input map[string]any) bool { return true }
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

	// Parse persona override if provided
	if persona, ok := input["persona"].(map[string]any); ok {
		sliderOverride := buildSliderOverride(persona)
		if sliderOverride != "" {
			// Prepend persona instructions to the goal so the subagent's
			// system prompt reflects the override. The subagent reads
			// slider.json from disk by default; we override by injecting
			// explicit voice modulation into the goal itself, which the
			// agent runtime prepends to the system context.
			goal = "[PERSONA OVERRIDE]\n" + sliderOverride + "\n[END PERSONA OVERRIDE]\n\n" + goal
		}
	}

	// Emit initial progress with the element name (assigned by Spawn)
	// We don't know the name yet, so emit a generic start first.
	if progress != nil {
		progress <- tools.ProgressEvent{
			ToolName: "spawn_agent",
			Message:  fmt.Sprintf("Spawning subagent: %s", truncate(goal, 80)),
		}
	}

	// Create a callback that streams subagent internal activity to
	// the parent's progress channel so the TUI can show nested turns.
	var turnCallback func(turn int, maxTurns int, toolName string)
	if progress != nil {
		turnCallback = func(turn int, maxTurns int, toolName string) {
			msg := fmt.Sprintf("turn %d/%d", turn, maxTurns)
			if toolName != "" {
				msg += " · " + toolName
			}
			progress <- tools.ProgressEvent{
				ToolName: "spawn_agent",
				Message:  msg,
			}
		}
	}

	run, err := t.manager.Spawn(ctx, goal, workspace, turnCallback)
	if err != nil {
		return tools.ToolResult{
			Content: fmt.Sprintf("Subagent failed: %v", err),
			Error:   true,
		}, nil
	}

	// Emit completion with element name
	if progress != nil {
		progress <- tools.ProgressEvent{
			ToolName: "spawn_agent",
			Message:  fmt.Sprintf("〔%s〕 completed %d turns", run.Name, run.Turns),
		}
	}

	// Format result with element identity
	result := fmt.Sprintf("〔%s〕 (%s) — completed in %d turns (%s)\n\n%s",
		run.Name, run.Element,
		run.Turns, run.EndedAt.Sub(run.StartedAt).Round(time.Millisecond),
		run.Result)

	return tools.ToolResult{
		Content: result,
		Metadata: map[string]any{
			"subagent_id":   run.ID,
			"subagent_name": run.Name,
			"element":       run.Element,
			"turns":         run.Turns,
			"status":        run.Status,
		},
	}, nil
}

// buildSliderOverride constructs a voice modulation prompt from the
// persona parameter map. Returns empty string if no overrides specified.
func buildSliderOverride(persona map[string]any) string {
	// Check for a named preset first
	if preset, ok := persona["preset"].(string); ok && preset != "" {
		sliders := config.LoadSliders()
		if sliders.LoadPreset(preset) {
			return prompts.ComposeSliderPrompt(sliders)
		}
	}

	// Build from individual slider values
	sliders := config.LoadSliders() // start from current defaults
	if v, ok := persona["flirt"].(float64); ok {
		sliders.Flirt = int(v)
	}
	if v, ok := persona["warmth"].(float64); ok {
		sliders.Warmth = int(v)
	}
	if v, ok := persona["register"].(float64); ok {
		sliders.Register = int(v)
	}
	if v, ok := persona["lewdness"].(float64); ok {
		sliders.Lewdness = int(v)
	}
	if v, ok := persona["r18"].(bool); ok {
		sliders.R18Enabled = v
	}

	return prompts.ComposeSliderPrompt(sliders)
}

// truncate shortens s to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
