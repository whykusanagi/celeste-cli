package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
	return "Spawn a subagent to handle a subtask. Returns its result when complete. " +
		"CRITICAL: For multi-step workflows where later steps depend on earlier steps (e.g., generate clips THEN mix them), " +
		"you MUST use task_id and depends_on parameters to create a DAG. " +
		"Example: spawn voice generation with task_id='voice', spawn SFX with task_id='sfx1', " +
		"then spawn the mixer with task_id='mix' and depends_on=['voice','sfx1']. " +
		"The mixer agent will WAIT until voice and sfx1 complete before starting. " +
		"Without depends_on, all agents run simultaneously and downstream agents will fail because files don't exist yet."
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
			"task_id": {
				"type": "string",
				"description": "Unique task identifier for DAG dependency references. Other subagents can depend on this ID via depends_on."
			},
			"depends_on": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Task IDs that must complete before this subagent starts. The subagent will wait in 'waiting' state until all dependencies finish, then auto-start with their results injected into its goal context."
			},
			"max_turns": {
				"type": "integer",
				"description": "Maximum agent turns before the subagent stops. Default 20. Increase for complex multi-step tasks (e.g., 40 for large content generation). Decrease for simple lookups (e.g., 5)."
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

	// Pre-peek the element name for the initial progress event.
	// The manager assigns names sequentially, so we can predict it.
	t.manager.mu.Lock()
	nextIdx := t.manager.counter
	var predictedName, predictedElement string
	if nextIdx < len(elementNames) {
		e := elementNames[nextIdx]
		predictedName = fmt.Sprintf("〔%s %s〕", e.Kanji, e.Romaji)
		predictedElement = e.Element
	} else {
		predictedName = fmt.Sprintf("〔第%d号〕", nextIdx+1)
		predictedElement = ""
	}
	t.manager.mu.Unlock()

	if progress != nil {
		progress <- tools.ProgressEvent{
			ToolName: "spawn_agent",
			Message:  fmt.Sprintf("%s spawning: %s", predictedName, truncate(goal, 60)),
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
				Percent:  float64(turn) / float64(maxTurns),
			}
		}
	}

	// Build spawn options with DAG dependencies
	spawnOpts := SpawnOptions{
		TurnCb: turnCallback,
	}

	// 1. Accept explicit params from the model
	if taskID, ok := input["task_id"].(string); ok && taskID != "" {
		spawnOpts.TaskID = taskID
	}
	if deps, ok := input["depends_on"].([]any); ok {
		for _, d := range deps {
			if depID, ok := d.(string); ok && depID != "" {
				spawnOpts.DependsOn = append(spawnOpts.DependsOn, depID)
			}
		}
	}
	if mt, ok := input["max_turns"].(float64); ok && mt > 0 {
		spawnOpts.MaxTurns = int(mt)
	}

	// 2. Extract task_id from goal text (grok embeds it there)
	if spawnOpts.TaskID == "" {
		if idx := strings.Index(goal, "task_id:"); idx >= 0 {
			rest := goal[idx+8:]
			end := len(rest)
			for i, c := range rest {
				if c == ' ' || c == '\n' || c == ',' || c == ']' || c == '}' {
					end = i
					break
				}
			}
			spawnOpts.TaskID = strings.TrimSpace(rest[:end])
		}
	}

	// 3. Auto-assign task_id from element name if still empty
	if spawnOpts.TaskID == "" && predictedElement != "" {
		spawnOpts.TaskID = predictedElement
	}

	// 4. Extract depends_on — explicit syntax from goal text
	if len(spawnOpts.DependsOn) == 0 {
		if idx := strings.Index(goal, "depends_on:["); idx >= 0 {
			rest := goal[idx+12:]
			endBracket := strings.Index(rest, "]")
			if endBracket > 0 {
				for _, part := range strings.Split(rest[:endBracket], ",") {
					dep := strings.Trim(strings.TrimSpace(part), "'\"")
					if dep != "" && !containsDep(spawnOpts.DependsOn, dep) {
						spawnOpts.DependsOn = append(spawnOpts.DependsOn, dep)
					}
				}
			}
		}
	}

	// 5. Auto-detect dependencies: handled inside SpawnWithOptions after
	// registration, where it has the lock and can see all registered runs.

	// Log DAG for visibility
	if spawnOpts.TaskID != "" && len(spawnOpts.DependsOn) > 0 {
		fmt.Fprintf(os.Stderr, "[DAG] %s depends_on: %v\n", spawnOpts.TaskID, spawnOpts.DependsOn)
	}

	// Emit waiting state if there are unmet dependencies
	if len(spawnOpts.DependsOn) > 0 && progress != nil {
		progress <- tools.ProgressEvent{
			ToolName: "spawn_agent",
			Message:  fmt.Sprintf("%s waiting for dependencies: %s", predictedName, strings.Join(spawnOpts.DependsOn, ", ")),
		}
	}

	run, err := t.manager.SpawnWithOptions(ctx, goal, workspace, spawnOpts)
	if err != nil {
		// Include partial results if the subagent made any progress
		content := fmt.Sprintf("Subagent failed: %v", err)
		meta := map[string]any{"status": "failed"}
		if run != nil {
			if run.Result != "" {
				content = fmt.Sprintf("〔%s〕 (%s) — FAILED after %d turns (%s)\n\n%s",
					run.Name, run.Element, run.Turns,
					run.EndedAt.Sub(run.StartedAt).Round(time.Millisecond),
					run.Result)
			}
			meta["subagent_id"] = run.ID
			meta["turns"] = run.Turns
		}
		return tools.ToolResult{
			Content:  content,
			Error:    true,
			Metadata: meta,
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
// containsWholeWord checks if s contains word as a whole word (not substring).
// e.g., "after step1 completes" contains "step1" but "step10" does not match "step1".
func containsWholeWord(s, word string) bool {
	idx := 0
	for {
		pos := strings.Index(s[idx:], word)
		if pos < 0 {
			return false
		}
		pos += idx
		// Check boundaries
		before := pos == 0 || !isWordChar(s[pos-1])
		after := pos+len(word) >= len(s) || !isWordChar(s[pos+len(word)])
		if before && after {
			return true
		}
		idx = pos + 1
	}
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func containsDep(deps []string, dep string) bool {
	for _, d := range deps {
		if strings.EqualFold(d, dep) {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
