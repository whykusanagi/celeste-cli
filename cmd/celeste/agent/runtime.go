package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

type Runner struct {
	client   *llm.Client
	registry *skills.Registry
	store    *CheckpointStore
	options  Options
	out      io.Writer
	errOut   io.Writer
}

func NewRunner(cfg *config.Config, options Options, out io.Writer, errOut io.Writer) (*Runner, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}

	if options.Workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		options.Workspace = cwd
	}
	absWorkspace, err := filepath.Abs(options.Workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace path: %w", err)
	}
	options.Workspace = filepath.Clean(absWorkspace)

	if options.MaxTurns <= 0 {
		options.MaxTurns = DefaultOptions().MaxTurns
	}
	if options.MaxToolCallsPerTurn <= 0 {
		options.MaxToolCallsPerTurn = DefaultOptions().MaxToolCallsPerTurn
	}
	if options.MaxConsecutiveNoToolTurns <= 0 {
		options.MaxConsecutiveNoToolTurns = DefaultOptions().MaxConsecutiveNoToolTurns
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = cfg.GetTimeout()
	}
	if options.ToolTimeout <= 0 {
		options.ToolTimeout = 45 * time.Second
	}
	if strings.TrimSpace(options.CompletionMarker) == "" {
		options.CompletionMarker = DefaultOptions().CompletionMarker
	}

	registry := skills.NewRegistry()
	if err := registry.LoadSkills(); err != nil {
		fmt.Fprintf(errOut, "Warning: failed to load custom skills: %v\n", err)
	}
	configLoader := config.NewConfigLoader(cfg)
	skills.RegisterBuiltinSkills(registry, configLoader)
	if err := RegisterDevSkills(registry, options.Workspace); err != nil {
		return nil, fmt.Errorf("register development skills: %w", err)
	}

	llmConfig := &llm.Config{
		APIKey:                cfg.APIKey,
		BaseURL:               cfg.BaseURL,
		Model:                 cfg.Model,
		Timeout:               cfg.GetTimeout(),
		SkipPersonaPrompt:     cfg.SkipPersonaPrompt,
		SimulateTyping:        cfg.SimulateTyping,
		TypingSpeed:           cfg.TypingSpeed,
		GoogleCredentialsFile: cfg.GoogleCredentialsFile,
		GoogleUseADC:          cfg.GoogleUseADC,
		Collections:           cfg.Collections,
		XAIFeatures:           cfg.XAIFeatures,
	}
	client := llm.NewClient(llmConfig, registry)

	systemPrompt := buildAgentSystemPrompt(options)
	if !cfg.SkipPersonaPrompt {
		systemPrompt = prompts.GetSystemPrompt(false) + "\n\n" + systemPrompt
	}
	client.SetSystemPrompt(systemPrompt)

	store, err := NewCheckpointStore("")
	if err != nil {
		return nil, err
	}

	return &Runner{
		client:   client,
		registry: registry,
		store:    store,
		options:  options,
		out:      out,
		errOut:   errOut,
	}, nil
}

func (r *Runner) ListRuns(limit int) ([]RunSummary, error) {
	return r.store.List(limit)
}

func (r *Runner) Resume(ctx context.Context, runID string) (*RunState, error) {
	state, err := r.store.Load(runID)
	if err != nil {
		return nil, err
	}
	if state.Options.Workspace == "" {
		state.Options.Workspace = r.options.Workspace
	}
	if state.Options.CompletionMarker == "" {
		state.Options.CompletionMarker = r.options.CompletionMarker
	}
	return r.runState(ctx, state)
}

func (r *Runner) RunGoal(ctx context.Context, goal string) (*RunState, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return nil, fmt.Errorf("goal is required")
	}

	state := NewRunState(goal, r.options)
	state.Messages = append(state.Messages, tui.ChatMessage{
		Role:      "user",
		Content:   goal,
		Timestamp: time.Now(),
	})
	state.Steps = append(state.Steps, Step{
		Turn:      0,
		Type:      "goal",
		Content:   goal,
		Timestamp: time.Now(),
	})

	return r.runState(ctx, state)
}

func (r *Runner) runState(ctx context.Context, state *RunState) (*RunState, error) {
	if state == nil {
		return nil, fmt.Errorf("run state is nil")
	}

	if !state.Options.DisableCheckpoints {
		if err := r.store.Save(state); err != nil {
			fmt.Fprintf(r.errOut, "Warning: failed to save checkpoint: %v\n", err)
		}
	}

	for state.Turn < state.Options.MaxTurns {
		state.Turn++
		state.Status = StatusRunning

		if state.Options.Verbose {
			fmt.Fprintf(r.out, "\n[agent] turn %d/%d\n", state.Turn, state.Options.MaxTurns)
		}

		requestCtx, cancel := context.WithTimeout(ctx, state.Options.RequestTimeout)
		result, err := r.client.SendMessageSync(requestCtx, state.Messages, r.client.GetSkills())
		cancel()
		if err != nil {
			state.Status = StatusFailed
			state.Error = err.Error()
			state.UpdatedAt = time.Now()
			_ = r.store.Save(state)
			return state, err
		}

		assistantMsg := tui.ChatMessage{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: convertToolCalls(result.ToolCalls),
			Timestamp: time.Now(),
		}
		state.Messages = append(state.Messages, assistantMsg)
		state.LastAssistantResponse = strings.TrimSpace(result.Content)
		state.Steps = append(state.Steps, Step{
			Turn:      state.Turn,
			Type:      "assistant",
			Content:   state.LastAssistantResponse,
			Timestamp: time.Now(),
		})

		if state.Options.Verbose && state.LastAssistantResponse != "" {
			fmt.Fprintf(r.out, "[assistant]\n%s\n", state.LastAssistantResponse)
		}

		if len(result.ToolCalls) == 0 {
			state.ConsecutiveNoToolTurns++
			if isCompletionResponse(state.LastAssistantResponse, state.Options) {
				completeState(state)
				if !state.Options.DisableCheckpoints {
					_ = r.store.Save(state)
				}
				return state, nil
			}

			if state.ConsecutiveNoToolTurns >= state.Options.MaxConsecutiveNoToolTurns {
				state.Status = StatusNoProgressStopped
				now := time.Now()
				state.CompletedAt = &now
				if !state.Options.DisableCheckpoints {
					_ = r.store.Save(state)
				}
				return state, nil
			}

			state.Messages = append(state.Messages, tui.ChatMessage{
				Role:      "user",
				Content:   buildContinuePrompt(state.Options),
				Timestamp: time.Now(),
			})

			if !state.Options.DisableCheckpoints {
				_ = r.store.Save(state)
			}
			continue
		}

		state.ConsecutiveNoToolTurns = 0
		toolCalls := result.ToolCalls
		if len(toolCalls) > state.Options.MaxToolCallsPerTurn {
			toolCalls = toolCalls[:state.Options.MaxToolCallsPerTurn]
		}

		for _, tc := range toolCalls {
			toolMsg := r.executeToolCall(ctx, state, tc)
			state.Messages = append(state.Messages, toolMsg)
			state.ToolCallCount++
		}

		if !state.Options.DisableCheckpoints {
			_ = r.store.Save(state)
		}
	}

	state.Status = StatusMaxTurnsReached
	now := time.Now()
	state.CompletedAt = &now
	if !state.Options.DisableCheckpoints {
		_ = r.store.Save(state)
	}
	return state, nil
}

func (r *Runner) executeToolCall(ctx context.Context, state *RunState, tc llm.ToolCallResult) tui.ChatMessage {
	toolName := tc.Name
	if state.Options.Verbose {
		fmt.Fprintf(r.out, "[tool] %s\n", toolName)
	}

	toolCtx, cancel := context.WithTimeout(ctx, state.Options.ToolTimeout)
	defer cancel()

	argsJSON := tc.Arguments
	resultContent := ""

	if !json.Valid([]byte(argsJSON)) {
		resultContent = fmt.Sprintf(`{"error": true, "message": "invalid tool arguments JSON", "tool": %q}`, toolName)
	} else {
		execution, err := r.client.ExecuteSkill(toolCtx, toolName, argsJSON)
		resultContent = formatToolResult(toolName, execution, err)
	}

	state.Steps = append(state.Steps, Step{
		Turn:      state.Turn,
		Type:      "tool",
		Name:      toolName,
		Content:   truncateForStep(resultContent),
		ToolCall:  tc.ID,
		Timestamp: time.Now(),
	})

	return tui.ChatMessage{
		Role:       "tool",
		ToolCallID: tc.ID,
		Name:       toolName,
		Content:    resultContent,
		Timestamp:  time.Now(),
	}
}

func completeState(state *RunState) {
	state.Status = StatusCompleted
	now := time.Now()
	state.CompletedAt = &now
	state.UpdatedAt = now
}

func isCompletionResponse(content string, options Options) bool {
	text := strings.TrimSpace(content)
	if text == "" {
		return false
	}
	if options.CompletionMarker != "" && strings.Contains(strings.ToUpper(text), strings.ToUpper(options.CompletionMarker)) {
		return true
	}
	return !options.RequireCompletionMarker
}

func buildContinuePrompt(options Options) string {
	marker := options.CompletionMarker
	if marker == "" {
		marker = "TASK_COMPLETE:"
	}
	return fmt.Sprintf("Continue working toward the goal. Use tools when needed. If you are done, respond with '%s' followed by final deliverables and validation notes.", marker)
}

func buildAgentSystemPrompt(options Options) string {
	marker := options.CompletionMarker
	if marker == "" {
		marker = "TASK_COMPLETE:"
	}

	return fmt.Sprintf(`You are Celeste Agent, an autonomous execution loop for software and content tasks.

Execution contract:
1. Work iteratively until the objective is complete.
2. Prefer using available tools to inspect files, search code, modify files, and validate outcomes.
3. Keep responses concise and action-focused.
4. When complete, begin your final response with %q and include:
   - what changed
   - what validations ran
   - any remaining risks/open items
5. If blocked, clearly describe the blocker and the next required user action.

Current workspace root: %s`, marker, options.Workspace)
}

func convertToolCalls(calls []llm.ToolCallResult) []tui.ToolCallInfo {
	if len(calls) == 0 {
		return nil
	}
	result := make([]tui.ToolCallInfo, 0, len(calls))
	for _, c := range calls {
		result = append(result, tui.ToolCallInfo{
			ID:        c.ID,
			Name:      c.Name,
			Arguments: c.Arguments,
		})
	}
	return result
}

func formatToolResult(toolName string, execution *skills.ExecutionResult, err error) string {
	if err != nil {
		payload, _ := json.Marshal(map[string]interface{}{
			"error":   true,
			"tool":    toolName,
			"message": err.Error(),
		})
		return string(payload)
	}
	if execution == nil {
		payload, _ := json.Marshal(map[string]interface{}{
			"error":   true,
			"tool":    toolName,
			"message": "nil execution result",
		})
		return string(payload)
	}
	if !execution.Success {
		payload, _ := json.Marshal(map[string]interface{}{
			"error":   true,
			"tool":    toolName,
			"message": execution.Error,
		})
		return string(payload)
	}

	switch v := execution.Result.(type) {
	case string:
		return v
	default:
		b, marshalErr := json.Marshal(v)
		if marshalErr != nil {
			payload, _ := json.Marshal(map[string]interface{}{
				"error":   true,
				"tool":    toolName,
				"message": fmt.Sprintf("failed to marshal tool result: %v", marshalErr),
			})
			return string(payload)
		}
		return string(b)
	}
}

func truncateForStep(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
