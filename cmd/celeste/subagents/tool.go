package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/pathutil"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/realroot"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
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

// Timeout gives a subagent 10 minutes; it manages its own turn limits.
func (t *SpawnAgentTool) Timeout() time.Duration { return 10 * time.Minute }

func (t *SpawnAgentTool) Description() string {
	return "Spawn a subagent to handle a subtask. Returns its result when complete. " +
		"CRITICAL: For multi-step workflows where later steps depend on earlier steps (e.g., generate clips THEN mix them), " +
		"you MUST use task_id and depends_on parameters to create a DAG. " +
		"Example: spawn voice generation with task_id='voice', spawn SFX with task_id='sfx1', " +
		"then spawn the mixer with task_id='mix' and depends_on=['voice','sfx1']. " +
		"The mixer agent will WAIT until voice and sfx1 complete before starting. " +
		"Without depends_on, all agents run simultaneously and downstream agents will fail because files don't exist yet. " +
		"Set type to explore (read-only investigation), review (code review) or general (the default: does the work); " +
		"the subagent's result comes back as JSON {summary, findings, files}."
}

func (t *SpawnAgentTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"goal": {
				"type": "string",
				"description": "A clear, self-contained description of what the subagent should accomplish"
			},
			"type": {
				"type": "string",
				"enum": ["explore", "general", "review"],
				"description": "Subagent type. explore: read-only tools, the persona off (identity and voice boundary only), the small model; for finding and reading things. review: read and code-graph tools, the persona off; for reviewing code. general (default): every tool and the persona; for doing the work. Every type finishes by calling submit_result with {summary, findings, files}, which comes back to you as JSON."
			},
			"workspace": {
				"type": "string",
				"description": "Working directory for the subagent: the current workspace or a directory inside it (defaults to the current workspace)"
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
			"isolate_worktree": {
				"type": "boolean",
				"description": "Run this subagent in its own isolated git worktree so concurrent subagents can't conflict on the same files. Results merge back to the parent branch on success; the worktree is removed afterward. Requires the workspace to be a git repo. Default false."
			},
			"background_after": {
				"type": "integer",
				"description": "Seconds to wait before auto-transitioning this subagent to the background. If it runs longer than this, the parent resumes immediately and the result is delivered when it finishes (visible via /agents). 0 (default) keeps it foreground/blocking."
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
	_, err := spawnType(input)
	return err
}

// spawnType reads and checks the type argument: an unknown type is an
// error, and explore and review refuse a persona override (2.0 W4e).
func spawnType(input map[string]any) (Type, error) {
	raw, present := input["type"]
	s, ok := raw.(string)
	if present && raw != nil && !ok {
		return "", fmt.Errorf("'type' must be a string: explore, general or review")
	}
	typ, err := ParseType(s)
	if err != nil {
		return "", err
	}
	// An empty or null persona asks for no override, so only a non-empty
	// one is refused.
	if p, has := input["persona"]; has && p != nil && !isEmptyMap(p) && (typ == TypeExplore || typ == TypeReview) {
		return "", fmt.Errorf("explore and review subagents run with the persona off, so they take no persona override; drop 'persona' or use type general")
	}
	return typ, nil
}

// scopeWorkspace resolves the model's workspace argument against the
// parent's workspace and refuses one outside it, symlinks resolved on both
// sides: a subagent's file tools, sandbox write root and auto-approval
// never reach past what the parent was given. An empty argument keeps the
// parent's workspace.
func scopeWorkspace(parent, ws string) (string, error) {
	if ws == "" {
		return "", nil
	}
	if parent == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("workspace %q: the parent workspace is unknown", ws)
		}
		parent = wd
	}
	parent, err := filepath.Abs(parent)
	if err != nil {
		return "", fmt.Errorf("workspace %q: %w", ws, err)
	}
	cand := ws
	if !filepath.IsAbs(cand) {
		cand = filepath.Join(parent, cand)
	}
	cand = filepath.Clean(cand)
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		realParent = parent
	}
	realCand, err := filepath.EvalSymlinks(cand)
	if err != nil {
		return "", fmt.Errorf("workspace %q: %w", ws, err)
	}
	if !pathutil.Within(realParent, realCand) {
		return "", fmt.Errorf("workspace %q is outside the current workspace; a subagent can only work inside it", ws)
	}
	// The resolved path is the one checked: returning cand would let a
	// symlink swapped in after the check redirect the subagent.
	return realCand, nil
}

// recheckWorkspace checks a subagent's workspace against the parent's
// again right before the subagent is built, and returns it resolved anew.
// scopeWorkspace's answer is only a path: a directory on it replaced by a
// symlink out of the parent since would otherwise lead the subagent's
// tools outside. The parent's own workspace (or none) needs no check.
//
// The directory checked is pinned: it is opened without following a
// symlink anywhere on its path, and stillPinned reports an error unless
// the path still opens, the same way, to that same directory. The caller
// calls it once the subagent is built and before anything runs in the
// workspace, so a rename-to-symlink after this check fails the run
// instead of redirecting it; the subagent's file writes open the
// workspace the same way for each change.
func recheckWorkspace(parent, ws string) (resolved string, stillPinned func() error, err error) {
	if parent == "" || ws == "" || filepath.Clean(ws) == filepath.Clean(parent) {
		return ws, func() error { return nil }, nil
	}
	resolved, err = scopeWorkspace(parent, ws)
	if err != nil {
		return "", nil, err
	}
	want, err := workspaceIdentity(resolved)
	if err != nil {
		return "", nil, fmt.Errorf("workspace %q: %w", ws, err)
	}
	return resolved, func() error {
		got, err := workspaceIdentity(resolved)
		if err != nil || !os.SameFile(want, got) {
			return fmt.Errorf("workspace %q changed after it was checked", ws)
		}
		return nil
	}, nil
}

// workspaceIdentity is the directory ws names, opened without following a
// symlink anywhere on its path.
func workspaceIdentity(ws string) (os.FileInfo, error) {
	r, err := realroot.Open(ws)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Stat(".")
}

func isEmptyMap(v any) bool {
	m, ok := v.(map[string]any)
	return ok && len(m) == 0
}

func (t *SpawnAgentTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	goal, _ := input["goal"].(string)
	workspace, _ := input["workspace"].(string)
	typ, err := spawnType(input)
	if err != nil {
		return tools.ToolResult{Content: err.Error(), Error: true}, nil
	}
	if workspace, err = scopeWorkspace(t.manager.workspace, workspace); err != nil {
		return tools.ToolResult{Content: err.Error(), Error: true}, nil
	}

	// A persona override replaces the slider block in the subagent's system
	// prompt. It used to be prepended to the goal, which left the subagent
	// with two conflicting Voice Modulation blocks (#170).
	var sliderOverride *config.SliderConfig
	if persona, ok := input["persona"].(map[string]any); ok {
		sliderOverride = buildSliderOverride(persona)
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
		TurnCb:  turnCallback,
		Sliders: sliderOverride,
		Type:    typ,
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
	if iso, ok := input["isolate_worktree"].(bool); ok {
		spawnOpts.IsolateWorktree = iso
	}
	if ba, ok := input["background_after"].(float64); ok && ba > 0 {
		spawnOpts.BackgroundAfter = time.Duration(ba) * time.Second
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
				if run.Type != "" && run.Summary != "" {
					// A typed run: the result JSON follows.
					content = fmt.Sprintf("subagent %s (%s): failed after %d turns: %v\n%s",
						run.Name, run.Type, run.Turns, err, run.Result)
				}
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

	// Background path: the run was returned still in flight (background_after
	// elapsed). It has no EndedAt/turns yet, so do NOT format it as "completed in
	// 0 turns (…)" — that misleads the model into thinking the spawn did nothing
	// and re-spawning in a loop (db9b9282). Tell it the agent is running and to
	// check /agents instead.
	if run.Status == "background" || run.Status == "running" {
		if progress != nil {
			progress <- tools.ProgressEvent{
				ToolName: "spawn_agent",
				Message:  fmt.Sprintf("〔%s〕 running in background", run.Name),
			}
		}
		bg := fmt.Sprintf("〔%s〕 (%s) is running in the background (id:%s). Do NOT spawn it again — check status with /agents, and cancel with /agents kill %s if needed. Its result will arrive when it finishes.",
			run.Name, run.Element, run.ID, run.Element)
		return tools.ToolResult{
			Content: bg,
			Metadata: map[string]any{
				"subagent_id":   run.ID,
				"subagent_name": run.Name,
				"element":       run.Element,
				"status":        run.Status,
				"background":    true,
			},
		}, nil
	}

	// Emit completion with element name
	if progress != nil {
		progress <- tools.ProgressEvent{
			ToolName: "spawn_agent",
			Message:  fmt.Sprintf("〔%s〕 completed %d turns", run.Name, run.Turns),
		}
	}

	// Format result with element identity. Guard the duration against a zero
	// EndedAt (defensive — a terminal run should always have it set).
	elapsed := "—"
	if !run.EndedAt.IsZero() {
		elapsed = run.EndedAt.Sub(run.StartedAt).Round(time.Millisecond).String()
	}
	result := fmt.Sprintf("〔%s〕 (%s) — completed in %d turns (%s)\n\n%s",
		run.Name, run.Element, run.Turns, elapsed, run.Result)
	if run.Type != "" {
		// A typed run (2.0 W4e ruling 6): one status line, then the result JSON.
		result = fmt.Sprintf("subagent %s (%s): %s\n%s", run.Name, run.Type, run.Status, run.Result)
	}

	return tools.ToolResult{
		Content: result,
		Metadata: map[string]any{
			"subagent_id":   run.ID,
			"subagent_name": run.Name,
			"element":       run.Element,
			"turns":         run.Turns,
			"status":        run.Status,
			"type":          string(run.Type),
		},
	}, nil
}

// buildSliderOverride builds the subagent's slider settings from the persona
// parameter map, starting from the user's slider.json. Returns nil when the
// map sets nothing, so the subagent uses slider.json as is.
func buildSliderOverride(persona map[string]any) *config.SliderConfig {
	if len(persona) == 0 {
		return nil
	}
	sliders := config.LoadSliders()
	userR18 := sliders.R18Enabled

	// A named preset takes precedence over individual values.
	if preset, ok := persona["preset"].(string); ok && preset != "" && sliders.LoadPreset(preset) {
		// A preset can't turn R18 on unless the user already has.
		sliders.R18Enabled = sliders.R18Enabled && userR18
		return sliders
	}

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
	// The model may turn R18 off for a subagent, never on: enabling it is the
	// user's decision (slider.json), not something a tool argument can grant (#171).
	if v, ok := persona["r18"].(bool); ok && !v {
		sliders.R18Enabled = false
	}
	return sliders
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
