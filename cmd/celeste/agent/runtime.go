package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

type Runner struct {
	client    *llm.Client
	registry  *skills.Registry
	store     *CheckpointStore
	options   Options
	out       io.Writer
	errOut    io.Writer
	eventSink EventSink
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
	normalizeOptions(&options)

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
	normalizeStateOptions(state, r.options)
	return r.runState(ctx, state)
}

func (r *Runner) RunGoal(ctx context.Context, goal string) (*RunState, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return nil, fmt.Errorf("goal is required")
	}

	state := NewRunState(goal, r.options)
	normalizeStateOptions(state, r.options)
	if !state.Options.EnablePlanning {
		state.Phase = PhaseExecution
	}

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
	normalizeStateOptions(state, r.options)
	defer r.persistArtifacts(state)
	r.emitEvent(state, "run_started", "Agent run started", nil)

	if state.Phase == "" {
		if state.Options.EnablePlanning {
			state.Phase = PhasePlanning
		} else {
			state.Phase = PhaseExecution
		}
	}

	if state.Phase == PhasePlanning {
		if err := r.runPlanningPhase(ctx, state); err != nil {
			state.Status = StatusFailed
			state.Error = err.Error()
			state.UpdatedAt = time.Now()
			r.emitEvent(state, "run_failed", fmt.Sprintf("Planning failed: %v", err), nil)
			_ = r.store.Save(state)
			return state, err
		}
		if !state.Options.DisableCheckpoints {
			if err := r.store.Save(state); err != nil {
				fmt.Fprintf(r.errOut, "Warning: failed to save checkpoint: %v\n", err)
			}
		}
	}

	for state.Turn < state.Options.MaxTurns {
		state.Turn++
		state.Status = StatusRunning
		state.Phase = PhaseExecution
		r.emitEvent(state, "turn_start", fmt.Sprintf("Turn %d started", state.Turn), map[string]any{
			"turn":      state.Turn,
			"max_turns": state.Options.MaxTurns,
		})

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
			r.emitEvent(state, "run_failed", fmt.Sprintf("LLM request failed: %v", err), nil)
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
		if state.LastAssistantResponse != "" {
			r.emitEvent(state, "assistant_response", truncateForStep(state.LastAssistantResponse), map[string]any{
				"turn": state.Turn,
			})
		}

		if len(result.ToolCalls) == 0 {
			state.ConsecutiveNoToolTurns++
			updatePlanProgressFromAssistant(state, state.LastAssistantResponse, false)
			r.emitEvent(state, "no_tool_turn", "Assistant returned no tool calls", map[string]any{
				"turn":                         state.Turn,
				"consecutive_no_tool_turns":    state.ConsecutiveNoToolTurns,
				"max_consecutive_no_tool_turn": state.Options.MaxConsecutiveNoToolTurns,
			})

			if state.Options.StopOnBlocker {
				if blockerReason := extractBlockerMarker(state.LastAssistantResponse, state.Options.BlockerMarker); blockerReason != "" {
					state.Status = StatusBlocked
					state.BlockerReason = blockerReason
					state.Error = blockerReason
					now := time.Now()
					state.CompletedAt = &now
					state.Steps = append(state.Steps, Step{
						Turn:      state.Turn,
						Type:      "blocked",
						Content:   truncateForStep(blockerReason),
						Timestamp: now,
					})
					r.emitEvent(state, "run_blocked", blockerReason, map[string]any{
						"marker": state.Options.BlockerMarker,
					})
					r.emitEvent(state, "run_stopped", blockerReason, map[string]any{
						"reason": "blocked",
					})
					if !state.Options.DisableCheckpoints {
						_ = r.store.Save(state)
					}
					return state, nil
				}
			}

			if isCompletionResponse(state.LastAssistantResponse, state.Options) {
				completed, err := r.handleCompletionCandidate(ctx, state)
				if err != nil {
					state.Status = StatusFailed
					state.Error = err.Error()
					state.UpdatedAt = time.Now()
					r.emitEvent(state, "run_failed", fmt.Sprintf("Completion handling failed: %v", err), nil)
					_ = r.store.Save(state)
					return state, err
				}
				if completed {
					r.emitEvent(state, "run_completed", "Agent run completed", nil)
					if !state.Options.DisableCheckpoints {
						_ = r.store.Save(state)
					}
					return state, nil
				}
			}

			if state.ConsecutiveNoToolTurns >= state.Options.MaxConsecutiveNoToolTurns {
				state.Status = StatusNoProgressStopped
				now := time.Now()
				state.CompletedAt = &now
				r.emitEvent(state, "run_stopped", "Stopped due to repeated no-progress turns", nil)
				if !state.Options.DisableCheckpoints {
					_ = r.store.Save(state)
				}
				return state, nil
			}

			state.Messages = append(state.Messages, tui.ChatMessage{
				Role:      "user",
				Content:   buildContinuePrompt(state),
				Timestamp: time.Now(),
			})

			if !state.Options.DisableCheckpoints {
				_ = r.store.Save(state)
			}
			continue
		}

		state.ConsecutiveNoToolTurns = 0
		updatePlanProgressFromAssistant(state, state.LastAssistantResponse, true)
		toolCalls := result.ToolCalls
		if len(toolCalls) > state.Options.MaxToolCallsPerTurn {
			toolCalls = toolCalls[:state.Options.MaxToolCallsPerTurn]
		}
		r.emitEvent(state, "tool_batch_start", fmt.Sprintf("Executing %d tool call(s)", len(toolCalls)), map[string]any{
			"turn":       state.Turn,
			"tool_calls": len(toolCalls),
		})

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
	r.emitEvent(state, "run_stopped", "Reached max turns", nil)
	if !state.Options.DisableCheckpoints {
		_ = r.store.Save(state)
	}
	return state, nil
}

func (r *Runner) runPlanningPhase(ctx context.Context, state *RunState) error {
	if !state.Options.EnablePlanning {
		state.Phase = PhaseExecution
		return nil
	}
	if len(state.Plan) > 0 {
		state.Phase = PhaseExecution
		markPlanStepInProgress(state, state.ActivePlanStep)
		return nil
	}

	state.Phase = PhasePlanning
	r.emitEvent(state, "planning_start", "Planning phase started", nil)
	prompt := buildPlanningPrompt(state)
	state.Messages = append(state.Messages, tui.ChatMessage{
		Role:      "user",
		Content:   prompt,
		Timestamp: time.Now(),
	})

	requestCtx, cancel := context.WithTimeout(ctx, state.Options.RequestTimeout)
	result, err := r.client.SendMessageSync(requestCtx, state.Messages, nil)
	cancel()
	if err != nil {
		return err
	}

	planResponse := strings.TrimSpace(result.Content)
	state.Messages = append(state.Messages, tui.ChatMessage{
		Role:      "assistant",
		Content:   planResponse,
		Timestamp: time.Now(),
	})
	state.Steps = append(state.Steps, Step{
		Turn:      state.Turn,
		Type:      "plan",
		Content:   truncateForStep(planResponse),
		Timestamp: time.Now(),
	})

	state.Plan = parsePlanSteps(planResponse, state.Options.PlanMaxSteps)
	if len(state.Plan) == 0 {
		state.Plan = []PlanStep{{Index: 1, Title: "Complete the requested goal", Status: PlanStatusPending}}
	}
	state.ActivePlanStep = 0
	markPlanStepInProgress(state, state.ActivePlanStep)
	state.Phase = PhaseExecution
	r.emitEvent(state, "planning_complete", fmt.Sprintf("Plan ready with %d step(s)", len(state.Plan)), map[string]any{
		"plan_steps": len(state.Plan),
	})

	state.Messages = append(state.Messages, tui.ChatMessage{
		Role:      "user",
		Content:   buildExecutionKickoffPrompt(state),
		Timestamp: time.Now(),
	})
	return nil
}

func (r *Runner) handleCompletionCandidate(ctx context.Context, state *RunState) (bool, error) {
	if !state.Options.RequireVerification || len(state.Options.VerificationCommands) == 0 {
		markAllPlanStepsCompleted(state)
		completeState(state)
		return true, nil
	}

	return r.runVerificationPhase(ctx, state)
}

func (r *Runner) runVerificationPhase(ctx context.Context, state *RunState) (bool, error) {
	state.Phase = PhaseVerification
	state.VerificationAttempts++
	r.emitEvent(state, "verification_start", "Verification phase started", map[string]any{
		"checks":            len(state.Options.VerificationCommands),
		"attempt":           state.VerificationAttempts,
		"max_retry_attempt": state.Options.MaxVerificationRetries,
	})
	state.Steps = append(state.Steps, Step{
		Turn:      state.Turn,
		Type:      "verification_start",
		Timestamp: time.Now(),
	})

	allPassed := true
	checks := make([]VerificationCheck, 0, len(state.Options.VerificationCommands))
	for _, cmd := range state.Options.VerificationCommands {
		check := executeVerificationCommand(ctx, state.Options.Workspace, cmd, state.Options.VerifyTimeout)
		checks = append(checks, check)
		r.emitEvent(state, "verification_check", fmt.Sprintf("Verification %s (passed=%v)", cmd, check.Passed), map[string]any{
			"command":  cmd,
			"passed":   check.Passed,
			"exitCode": check.ExitCode,
			"timedOut": check.TimedOut,
		})
		state.Steps = append(state.Steps, Step{
			Turn:      state.Turn,
			Type:      "verification_check",
			Name:      cmd,
			Content:   truncateForStep(check.Output),
			Timestamp: time.Now(),
		})
		if !check.Passed {
			allPassed = false
		}
	}
	state.Verification = append(state.Verification, checks...)

	if allPassed {
		markAllPlanStepsCompleted(state)
		completeState(state)
		r.emitEvent(state, "verification_passed", "Verification checks passed", nil)
		r.emitEvent(state, "run_completed", "Agent run completed", nil)
		state.Steps = append(state.Steps, Step{
			Turn:      state.Turn,
			Type:      "verification_passed",
			Timestamp: time.Now(),
		})
		return true, nil
	}

	if state.VerificationAttempts >= state.Options.MaxVerificationRetries {
		state.Status = StatusVerificationStop
		state.Error = fmt.Sprintf("verification failed after %d attempt(s)", state.VerificationAttempts)
		now := time.Now()
		state.CompletedAt = &now
		state.Steps = append(state.Steps, Step{
			Turn:      state.Turn,
			Type:      "verification_exhausted",
			Content:   truncateForStep(state.Error),
			Timestamp: now,
		})
		r.emitEvent(state, "verification_exhausted", state.Error, nil)
		r.emitEvent(state, "run_stopped", state.Error, nil)
		return true, nil
	}

	failureSummary := buildVerificationFailurePrompt(checks, state.Options, state.VerificationAttempts)
	state.Messages = append(state.Messages, tui.ChatMessage{
		Role:      "user",
		Content:   failureSummary,
		Timestamp: time.Now(),
	})
	state.Steps = append(state.Steps, Step{
		Turn:      state.Turn,
		Type:      "verification_failed",
		Content:   truncateForStep(failureSummary),
		Timestamp: time.Now(),
	})
	r.emitEvent(state, "verification_failed", "Verification failed; continuing execution", map[string]any{
		"attempt":     state.VerificationAttempts,
		"max_attempt": state.Options.MaxVerificationRetries,
	})
	state.ConsecutiveNoToolTurns = 0
	state.Phase = PhaseExecution
	return false, nil
}

func (r *Runner) executeToolCall(ctx context.Context, state *RunState, tc llm.ToolCallResult) tui.ChatMessage {
	toolName := tc.Name
	r.emitEvent(state, "tool_start", fmt.Sprintf("Executing tool: %s", toolName), map[string]any{
		"turn":         state.Turn,
		"tool_name":    toolName,
		"tool_call_id": tc.ID,
	})
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
	r.emitEvent(state, "tool_result", fmt.Sprintf("Tool completed: %s", toolName), map[string]any{
		"turn":         state.Turn,
		"tool_name":    toolName,
		"tool_call_id": tc.ID,
	})

	return tui.ChatMessage{
		Role:       "tool",
		ToolCallID: tc.ID,
		Name:       toolName,
		Content:    resultContent,
		Timestamp:  time.Now(),
	}
}

func executeVerificationCommand(parent context.Context, workspace, command string, timeout time.Duration) VerificationCheck {
	if timeout <= 0 {
		timeout = DefaultOptions().VerifyTimeout
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()

	outputStr := string(output)
	if len(outputStr) > maxCommandOutput {
		outputStr = outputStr[:maxCommandOutput]
	}

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	timedOut := ctx.Err() == context.DeadlineExceeded
	passed := err == nil && !timedOut

	return VerificationCheck{
		Command:   command,
		Passed:    passed,
		ExitCode:  exitCode,
		Output:    outputStr,
		TimedOut:  timedOut,
		Timestamp: time.Now(),
	}
}

func normalizeOptions(options *Options) {
	defaults := DefaultOptions()
	if options.MaxTurns <= 0 {
		options.MaxTurns = defaults.MaxTurns
	}
	if options.MaxToolCallsPerTurn <= 0 {
		options.MaxToolCallsPerTurn = defaults.MaxToolCallsPerTurn
	}
	if options.MaxConsecutiveNoToolTurns <= 0 {
		options.MaxConsecutiveNoToolTurns = defaults.MaxConsecutiveNoToolTurns
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = defaults.RequestTimeout
	}
	if options.ToolTimeout <= 0 {
		options.ToolTimeout = defaults.ToolTimeout
	}
	if strings.TrimSpace(options.CompletionMarker) == "" {
		options.CompletionMarker = defaults.CompletionMarker
	}
	if options.PlanMaxSteps <= 0 {
		options.PlanMaxSteps = defaults.PlanMaxSteps
	}
	if options.VerifyTimeout <= 0 {
		options.VerifyTimeout = defaults.VerifyTimeout
	}
	if options.MaxVerificationRetries <= 0 {
		options.MaxVerificationRetries = defaults.MaxVerificationRetries
	}
	if strings.TrimSpace(options.BlockerMarker) == "" {
		options.BlockerMarker = defaults.BlockerMarker
	}
}

func normalizeStateOptions(state *RunState, fallback Options) {
	if state.Options.Workspace == "" {
		state.Options.Workspace = fallback.Workspace
	}
	if state.Options.ArtifactDir == "" && fallback.ArtifactDir != "" {
		state.Options.ArtifactDir = fallback.ArtifactDir
	}
	if state.Options.VerificationCommands == nil && fallback.VerificationCommands != nil {
		state.Options.VerificationCommands = fallback.VerificationCommands
	}
	if len(state.Options.VerificationCommands) == 0 && len(fallback.VerificationCommands) > 0 {
		state.Options.VerificationCommands = fallback.VerificationCommands
	}
	if !state.Options.EnablePlanning && fallback.EnablePlanning {
		state.Options.EnablePlanning = fallback.EnablePlanning
	}
	if !state.Options.RequireVerification && fallback.RequireVerification {
		state.Options.RequireVerification = fallback.RequireVerification
	}
	if state.Options.MaxVerificationRetries <= 0 && fallback.MaxVerificationRetries > 0 {
		state.Options.MaxVerificationRetries = fallback.MaxVerificationRetries
	}
	if !state.Options.StopOnBlocker && fallback.StopOnBlocker {
		state.Options.StopOnBlocker = fallback.StopOnBlocker
	}
	if strings.TrimSpace(state.Options.BlockerMarker) == "" && strings.TrimSpace(fallback.BlockerMarker) != "" {
		state.Options.BlockerMarker = fallback.BlockerMarker
	}
	if !state.Options.EmitArtifacts && fallback.EmitArtifacts {
		state.Options.EmitArtifacts = fallback.EmitArtifacts
	}
	normalizeOptions(&state.Options)
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

func buildPlanningPrompt(state *RunState) string {
	return fmt.Sprintf("Create a concise execution plan for this goal with 3-7 numbered steps. Include technical validation steps. Goal: %s", state.Goal)
}

func buildExecutionKickoffPrompt(state *RunState) string {
	planLines := make([]string, 0, len(state.Plan))
	for _, step := range state.Plan {
		planLines = append(planLines, fmt.Sprintf("%d. %s", step.Index, step.Title))
	}
	return fmt.Sprintf("Begin executing this plan now. Use tools as needed and emit STEP_DONE: <n> when a step is completed.\n\nPlan:\n%s", strings.Join(planLines, "\n"))
}

func buildContinuePrompt(state *RunState) string {
	marker := state.Options.CompletionMarker
	if marker == "" {
		marker = "TASK_COMPLETE:"
	}
	blockerMarker := strings.TrimSpace(state.Options.BlockerMarker)
	if blockerMarker == "" {
		blockerMarker = "BLOCKED:"
	}

	stepHint := ""
	if len(state.Plan) > 0 && state.ActivePlanStep >= 0 && state.ActivePlanStep < len(state.Plan) {
		stepHint = fmt.Sprintf(" Focus on plan step %d: %s.", state.Plan[state.ActivePlanStep].Index, state.Plan[state.ActivePlanStep].Title)
	}
	return fmt.Sprintf("Continue working toward the goal.%s Use tools when needed. If you are blocked, respond with '%s <reason>'. If you are done, respond with '%s' followed by final deliverables and validation notes.", stepHint, blockerMarker, marker)
}

func buildVerificationFailurePrompt(checks []VerificationCheck, options Options, attempt int) string {
	failed := make([]string, 0)
	for _, c := range checks {
		if c.Passed {
			continue
		}
		failed = append(failed, fmt.Sprintf("- `%s` (exit=%d, timed_out=%v)\n%s", c.Command, c.ExitCode, c.TimedOut, truncateForStep(c.Output)))
	}
	return fmt.Sprintf("Verification failed (attempt %d/%d). Fix the issues and continue execution. Re-run validations before completion.\n\nFailed checks:\n%s\n\nWhen complete, respond with '%s'.", attempt, options.MaxVerificationRetries, strings.Join(failed, "\n"), options.CompletionMarker)
}

func buildAgentSystemPrompt(options Options) string {
	marker := options.CompletionMarker
	if marker == "" {
		marker = "TASK_COMPLETE:"
	}

	verificationInstruction := ""
	if options.RequireVerification && len(options.VerificationCommands) > 0 {
		verificationInstruction = fmt.Sprintf("Before final completion, ensure verification commands pass. You have at most %d verification attempts.", options.MaxVerificationRetries)
	}
	blockerInstruction := ""
	if options.StopOnBlocker {
		blockerMarker := strings.TrimSpace(options.BlockerMarker)
		if blockerMarker == "" {
			blockerMarker = "BLOCKED:"
		}
		blockerInstruction = fmt.Sprintf("If you are blocked, respond with '%s <reason>'.", blockerMarker)
	}

	return fmt.Sprintf(`You are Celeste Agent, an autonomous execution loop for software and content tasks.

Execution contract:
1. Work iteratively until the objective is complete.
2. Prefer using available tools to inspect files, search code, modify files, and validate outcomes.
3. Keep responses concise and action-focused.
4. Use explicit progress markers: STEP_DONE: <n> when you complete plan step n.
5. When complete, begin your final response with %q and include:
   - what changed
   - what validations ran
   - any remaining risks/open items
6. If blocked, clearly describe the blocker and the next required user action.
7. %s
8. %s

Current workspace root: %s`, marker, verificationInstruction, blockerInstruction, options.Workspace)
}

func parsePlanSteps(content string, maxSteps int) []PlanStep {
	if maxSteps <= 0 {
		maxSteps = DefaultOptions().PlanMaxSteps
	}

	lines := strings.Split(content, "\n")
	steps := make([]PlanStep, 0, maxSteps)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		title := ""
		if isNumberedStep(line) {
			title = stripStepPrefix(line)
		} else if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			title = strings.TrimSpace(line[2:])
		}

		if title == "" {
			continue
		}
		steps = append(steps, PlanStep{
			Index:  len(steps) + 1,
			Title:  title,
			Status: PlanStatusPending,
		})
		if len(steps) >= maxSteps {
			break
		}
	}
	return steps
}

func isNumberedStep(line string) bool {
	if len(line) < 3 {
		return false
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(line) {
		return false
	}
	if line[i] != '.' && line[i] != ')' && line[i] != ':' {
		return false
	}
	if i+1 >= len(line) {
		return false
	}
	return line[i+1] == ' '
}

func stripStepPrefix(line string) string {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i >= len(line) {
		return strings.TrimSpace(line)
	}
	if (line[i] == '.' || line[i] == ')' || line[i] == ':') && i+1 < len(line) {
		return strings.TrimSpace(line[i+1:])
	}
	return strings.TrimSpace(line)
}

func extractStepDoneMarker(content string) int {
	upper := strings.ToUpper(content)
	idx := strings.Index(upper, "STEP_DONE:")
	if idx < 0 {
		return -1
	}
	rest := strings.TrimSpace(content[idx+len("STEP_DONE:"):])
	numBuf := strings.Builder{}
	for _, r := range rest {
		if r >= '0' && r <= '9' {
			numBuf.WriteRune(r)
		} else {
			break
		}
	}
	if numBuf.Len() == 0 {
		return -1
	}
	n, err := strconv.Atoi(numBuf.String())
	if err != nil || n <= 0 {
		return -1
	}
	return n
}

func extractBlockerMarker(content string, marker string) string {
	marker = strings.TrimSpace(marker)
	if marker == "" {
		marker = "BLOCKED:"
	}

	upperContent := strings.ToUpper(content)
	upperMarker := strings.ToUpper(marker)
	idx := strings.Index(upperContent, upperMarker)
	if idx < 0 {
		return ""
	}

	rest := strings.TrimSpace(content[idx+len(marker):])
	if rest == "" {
		return "blocked without details"
	}
	line := rest
	if newline := strings.Index(line, "\n"); newline >= 0 {
		line = line[:newline]
	}
	return strings.TrimSpace(line)
}

func updatePlanProgressFromAssistant(state *RunState, content string, hadTools bool) {
	if len(state.Plan) == 0 {
		return
	}

	if step := extractStepDoneMarker(content); step > 0 {
		markPlanStepsCompletedThrough(state, step-1)
		next := step
		if next >= len(state.Plan) {
			next = len(state.Plan) - 1
		}
		state.ActivePlanStep = next
		if state.ActivePlanStep >= 0 && state.ActivePlanStep < len(state.Plan) {
			if state.Plan[state.ActivePlanStep].Status != PlanStatusCompleted {
				state.Plan[state.ActivePlanStep].Status = PlanStatusInProgress
			}
		}
		return
	}

	if hadTools {
		markPlanStepInProgress(state, state.ActivePlanStep)
	}
}

func markPlanStepsCompletedThrough(state *RunState, idx int) {
	if idx < 0 {
		return
	}
	if idx >= len(state.Plan) {
		idx = len(state.Plan) - 1
	}
	for i := 0; i <= idx; i++ {
		state.Plan[i].Status = PlanStatusCompleted
	}
}

func markPlanStepInProgress(state *RunState, idx int) {
	if len(state.Plan) == 0 {
		return
	}
	if idx < 0 || idx >= len(state.Plan) {
		idx = 0
		state.ActivePlanStep = 0
	}
	if state.Plan[idx].Status == PlanStatusPending {
		state.Plan[idx].Status = PlanStatusInProgress
	}
}

func markAllPlanStepsCompleted(state *RunState) {
	for i := range state.Plan {
		state.Plan[i].Status = PlanStatusCompleted
	}
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
