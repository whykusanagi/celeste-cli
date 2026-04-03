package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

const maxCommandOutput = 12_000

type Runner struct {
	client   *llm.Client
	registry *tools.Registry
	store    *CheckpointStore
	options  Options
	out      io.Writer
	errOut   io.Writer
	budget   *ctxmgr.TokenBudget
	indexer  *codegraph.Indexer // code graph indexer, may be nil
}

// emitProgress calls r.options.OnProgress if it is set.
func (r *Runner) emitProgress(kind ProgressKind, text string, turn, maxTurns int) {
	if r.options.OnProgress != nil {
		r.options.OnProgress(kind, text, turn, maxTurns)
	}
}

// Close releases resources held by the runner (e.g. code graph DB).
func (r *Runner) Close() {
	if r.indexer != nil {
		r.indexer.Close()
	}
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

	// Set up file checkpointing for stale detection and undo support.
	fileTracker := checkpoints.NewFileTracker()
	sessionID := fmt.Sprintf("agent-%d", os.Getpid())
	snapshotMgr := checkpoints.NewSnapshotManager(sessionID)

	// Agent registry: register dev tools only (no configLoader = no skill tools).
	registry := tools.NewRegistry()
	builtin.RegisterAll(registry, options.Workspace, nil, fileTracker, snapshotMgr)

	// Initialize code graph for the workspace
	var cgIndexer *codegraph.Indexer
	_ = os.MkdirAll(filepath.Join(options.Workspace, ".celeste"), 0755)
	if idx, cgErr := codegraph.NewIndexer(options.Workspace, filepath.Join(options.Workspace, ".celeste", "codegraph.db")); cgErr != nil {
		fmt.Fprintf(errOut, "Warning: code graph init failed: %v\n", cgErr)
	} else {
		if err := idx.Update(); err != nil {
			fmt.Fprintf(errOut, "Warning: code graph update failed: %v\n", err)
		}
		builtin.RegisterCodeGraphTools(registry, idx)
		cgIndexer = idx
	}

	// Load permissions and set checker
	agentHomeDir, _ := os.UserHomeDir()
	permConfigPath := filepath.Join(agentHomeDir, ".celeste", "permissions.json")
	permConfig, permErr := permissions.LoadConfig(permConfigPath)
	if permErr != nil {
		defaultCfg := permissions.DefaultConfig()
		permConfig = &defaultCfg
	}
	registry.SetPermissionChecker(permissions.NewChecker(*permConfig))

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

	// Build system prompt. The agent operational rules always come last so they
	// take precedence over character voice. The persona (if enabled) sets tone
	// only — tool-use rules in the agent prompt override any conflicting phrasing.
	envContext := detectEnvContext()
	systemPrompt := buildAgentSystemPrompt(options, envContext)
	if !cfg.SkipPersonaPrompt {
		persona := prompts.GetSystemPrompt(false)
		if persona != "" {
			systemPrompt = persona + "\n\n" + systemPrompt
		}
	}

	// Inject grimoire and git context into agent system prompt
	if projectGrimoire, err := grimoire.LoadAll(options.Workspace); err == nil && projectGrimoire != nil && !projectGrimoire.IsEmpty() {
		systemPrompt += "\n\n# Project Context (.grimoire)\n\n" + projectGrimoire.Render()
	}
	if gitSnap := grimoire.CaptureGitSnapshot(options.Workspace); gitSnap != nil {
		systemPrompt += "\n\n" + gitSnap.FormatForPrompt()
	}

	client.SetSystemPrompt(systemPrompt)

	store, err := NewCheckpointStore("")
	if err != nil {
		return nil, err
	}

	// Create a token budget for context tracking.
	systemPromptTokens := ctxmgr.EstimateTokens(systemPrompt)
	budget := ctxmgr.NewTokenBudgetForModel(cfg.Model, systemPromptTokens, 0)

	return &Runner{
		client:   client,
		registry: registry,
		store:    store,
		options:  options,
		out:      out,
		errOut:   errOut,
		budget:   budget,
		indexer:  cgIndexer,
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
			_ = r.store.Save(state)
			r.emitProgress(ProgressError, err.Error(), state.Turn, state.Options.MaxTurns)
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

		if state.Options.Verbose {
			fmt.Fprintf(r.out, "\n[agent] turn %d/%d\n", state.Turn, state.Options.MaxTurns)
		}
		r.emitProgress(ProgressTurnStart, fmt.Sprintf("turn %d/%d", state.Turn, state.Options.MaxTurns), state.Turn, state.Options.MaxTurns)

		requestCtx, cancel := context.WithTimeout(ctx, state.Options.RequestTimeout)
		turnStart := time.Now()

		// Use streaming events to collect the response incrementally.
		// This is the bridge approach: we collect tool calls via the
		// ToolUseAccumulator but still execute them serially below.
		var result llm.ChatCompletionResult
		acc := llm.NewToolUseAccumulator()

		streamErr := r.client.SendMessageStreamEvents(requestCtx, state.Messages, r.client.GetSkills(), func(event llm.StreamEvent) {
			switch event.Type {
			case llm.EventContentDelta:
				result.Content += event.ContentDelta
			case llm.EventToolUseStart, llm.EventToolUseInputDelta, llm.EventToolUseDone:
				acc.HandleEvent(event)
			case llm.EventMessageDone:
				result.Usage = event.Usage
			}
		})
		cancel()

		if streamErr != nil {
			state.Status = StatusFailed
			state.Error = streamErr.Error()
			state.UpdatedAt = time.Now()
			_ = r.store.Save(state)
			r.emitProgress(ProgressError, streamErr.Error(), state.Turn, state.Options.MaxTurns)
			return state, streamErr
		}

		result.ToolCalls = acc.CompletedCalls()

		// Update token budget with usage from this turn.
		if r.budget != nil && result.Usage != nil {
			r.budget.AddTurn(result.Usage.PromptTokens, result.Usage.CompletionTokens)
			if r.budget.ShouldCompactReactive() {
				fmt.Fprintf(r.errOut, "[agent] warning: context usage at %.0f%% — compaction recommended\n",
					r.budget.GetUsagePercent()*100)
			}
		}

		if r.options.OnTurnStats != nil {
			stats := TurnStats{Turn: state.Turn, MaxTurns: state.Options.MaxTurns, Elapsed: time.Since(turnStart)}
			if result.Usage != nil {
				stats.InputTokens = result.Usage.PromptTokens
				stats.OutputTokens = result.Usage.CompletionTokens
			}
			stats.Response = strings.TrimSpace(result.Content)
			if len(result.ToolCalls) > 0 {
				stats.ToolCalls = make([]string, len(result.ToolCalls))
				for i, tc := range result.ToolCalls {
					stats.ToolCalls[i] = tc.Name
				}
			}
			r.options.OnTurnStats(stats)
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

		// Text-based tool call fallback: some proxies/models don't issue native
		// API tool_calls but describe them inline. Parse <tool_call>...</tool_call>
		// blocks from the response content so we can execute them.
		if len(result.ToolCalls) == 0 && result.Content != "" {
			if textCalls := parseTextToolCalls(result.Content); len(textCalls) > 0 {
				result.ToolCalls = textCalls
			}
		}

		if len(result.ToolCalls) == 0 {
			state.ConsecutiveNoToolTurns++
			updatePlanProgressFromAssistant(state, state.LastAssistantResponse, false)

			if isCompletionResponse(state.LastAssistantResponse, state.Options) {
				completed, err := r.handleCompletionCandidate(ctx, state)
				if err != nil {
					state.Status = StatusFailed
					state.Error = err.Error()
					state.UpdatedAt = time.Now()
					_ = r.store.Save(state)
					r.emitProgress(ProgressError, err.Error(), state.Turn, state.Options.MaxTurns)
					return state, err
				}
				if completed {
					r.emitProgress(ProgressResponse, state.LastAssistantResponse, state.Turn, state.Options.MaxTurns)
					if !state.Options.DisableCheckpoints {
						_ = r.store.Save(state)
					}
					r.emitProgress(ProgressComplete, state.Status, state.Turn, state.Options.MaxTurns)
					return state, nil
				}
			}

			if state.ConsecutiveNoToolTurns >= state.Options.MaxConsecutiveNoToolTurns {
				state.Status = StatusNoProgressStopped
				now := time.Now()
				state.CompletedAt = &now
				if !state.Options.DisableCheckpoints {
					_ = r.store.Save(state)
				}
				r.emitProgress(ProgressComplete, state.Status, state.Turn, state.Options.MaxTurns)
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

		for _, tc := range toolCalls {
			r.emitProgress(ProgressToolCall, tc.Name, state.Turn, state.Options.MaxTurns)
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
	r.emitProgress(ProgressComplete, state.Status, state.Turn, state.Options.MaxTurns)
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
	prompt := buildPlanningPrompt(state)
	state.Messages = append(state.Messages, tui.ChatMessage{
		Role:      "user",
		Content:   prompt,
		Timestamp: time.Now(),
	})

	requestCtx, cancel := context.WithTimeout(ctx, state.Options.RequestTimeout)
	planTurnStart := time.Now()

	var result llm.ChatCompletionResult
	streamErr := r.client.SendMessageStreamEvents(requestCtx, state.Messages, r.client.GetSkills(), func(event llm.StreamEvent) {
		switch event.Type {
		case llm.EventContentDelta:
			result.Content += event.ContentDelta
		case llm.EventMessageDone:
			result.Usage = event.Usage
		}
	})
	cancel()
	if streamErr != nil {
		return streamErr
	}

	if r.options.OnTurnStats != nil {
		stats := TurnStats{Turn: state.Turn, MaxTurns: state.Options.MaxTurns, Elapsed: time.Since(planTurnStart)}
		if result.Usage != nil {
			stats.InputTokens = result.Usage.PromptTokens
			stats.OutputTokens = result.Usage.CompletionTokens
		}
		r.options.OnTurnStats(stats)
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
		state.Steps = append(state.Steps, Step{
			Turn:      state.Turn,
			Type:      "verification_passed",
			Timestamp: time.Now(),
		})
		return true, nil
	}

	failureSummary := buildVerificationFailurePrompt(checks, state.Options)
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
	state.ConsecutiveNoToolTurns = 0
	state.Phase = PhaseExecution
	return false, nil
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

	// Text-based tool calls have IDs like "text-tc-N"; they don't have
	// matching tool_call_id entries in the assistant message, so we use
	// "user" role with a labelled result instead of the "tool" role.
	if strings.HasPrefix(tc.ID, "text-tc-") {
		return tui.ChatMessage{
			Role:      "user",
			Content:   fmt.Sprintf("[Tool Result: %s]\n%s", toolName, resultContent),
			Timestamp: time.Now(),
		}
	}

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

	stepHint := ""
	if len(state.Plan) > 0 && state.ActivePlanStep >= 0 && state.ActivePlanStep < len(state.Plan) {
		stepHint = fmt.Sprintf(" Focus on plan step %d: %s.", state.Plan[state.ActivePlanStep].Index, state.Plan[state.ActivePlanStep].Title)
	}
	return fmt.Sprintf("Continue working toward the goal.%s Use tools when needed. If you are done, respond with '%s' followed by final deliverables and validation notes.", stepHint, marker)
}

func buildVerificationFailurePrompt(checks []VerificationCheck, options Options) string {
	failed := make([]string, 0)
	for _, c := range checks {
		if c.Passed {
			continue
		}
		failed = append(failed, fmt.Sprintf("- `%s` (exit=%d, timed_out=%v)\n%s", c.Command, c.ExitCode, c.TimedOut, truncateForStep(c.Output)))
	}
	return fmt.Sprintf("Verification failed. Fix the issues and continue execution. Re-run validations before completion.\n\nFailed checks:\n%s\n\nWhen complete, respond with '%s'.", strings.Join(failed, "\n"), options.CompletionMarker)
}

// detectEnvContext probes the runtime environment and returns a concise
// summary string for inclusion in the agent system prompt.
func detectEnvContext() string {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macOS"
	}
	arch := runtime.GOARCH

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "unknown"
	}

	pkgManager := "none"
	for _, candidate := range []string{"brew", "apt-get", "apt", "dnf", "yum", "pacman", "apk", "scoop", "choco"} {
		if path, err := exec.LookPath(candidate); err == nil {
			pkgManager = filepath.Base(path)
			break
		}
	}

	pythonExe := "none"
	for _, candidate := range []string{"python3", "python"} {
		if _, err := exec.LookPath(candidate); err == nil {
			pythonExe = candidate
			break
		}
	}

	return fmt.Sprintf("OS: %s (%s)\nShell: %s\nPackage manager: %s\nPython: %s",
		osName, arch, shell, pkgManager, pythonExe)
}

func buildAgentSystemPrompt(options Options, envContext string) string {
	marker := options.CompletionMarker
	if marker == "" {
		marker = "TASK_COMPLETE:"
	}

	verificationInstruction := ""
	if options.RequireVerification && len(options.VerificationCommands) > 0 {
		verificationInstruction = "Before final completion, run all verification commands using dev_run_command and confirm they pass."
	}

	return fmt.Sprintf(`You are Celeste Agent, an autonomous execution loop for software and content tasks.

## Tool Usage — Non-Negotiable Rules

You have file and shell tools. You MUST use them. There are no exceptions.

- To read a file: call dev_read_file. Never ask the user to paste contents.
- To write a new file: call dev_write_file. NEVER output file content as raw text in your response.
- To edit an existing file: call dev_patch_file with old_string/new_string. Never rewrite the whole file unless it is new.
- To run a command (git status, go test, ls, grep, etc.): call dev_run_command.
- To find files: call dev_list_files or dev_run_command with ls/find.
- To search code: call dev_run_command with grep, or dev_search_files.

## Tool Invocation Format

Invoke tools via the function calling API when available. If the API does not forward function calls, use this exact text format instead — one block per tool:

<tool_call>{"name": "dev_write_file", "arguments": {"path": "hello.py", "content": "print('hello')"}}</tool_call>

Rules for text-format tool calls:
- Output ONLY the <tool_call> block(s) — do NOT narrate the action or simulate the output.
- Stop after the block(s). Wait for [Tool Result] messages before continuing.
- Do NOT write "I will call...", "Let me...", or any description before or after the block.

If you write code or file content in your response instead of calling a tool, you have failed. The content will appear in the chat and nothing will be written to disk.

## Execution Contract

1. Work iteratively — inspect, act, verify — until the objective is complete.
2. Emit STEP_DONE: <n> when you complete plan step n.
3. When complete, begin your final response with %q and include:
   - what files were created or modified
   - what commands ran and their results
   - any remaining risks or open items
4. If blocked, clearly describe the blocker and what the user needs to do.
5. %s

## Environment

%s

Use the package manager and Python executable listed above. Do not use sudo or assume alternatives are available.

Workspace root: %s`, marker, verificationInstruction, envContext, options.Workspace)
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

// parseTextToolCalls extracts <tool_call>...</tool_call> blocks from text.
// This provides a fallback for models/proxies that understand tools but don't
// issue native API tool_calls — they emit the invocation as structured text.
// The expected block format is:
//
//	<tool_call>{"name":"dev_write_file","arguments":{"path":"x","content":"y"}}</tool_call>
func parseTextToolCalls(content string) []llm.ToolCallResult {
	var results []llm.ToolCallResult
	remaining := content
	for {
		start := strings.Index(remaining, "<tool_call>")
		if start < 0 {
			break
		}
		after := remaining[start+len("<tool_call>"):]
		end := strings.Index(after, "</tool_call>")
		if end < 0 {
			break
		}
		jsonStr := strings.TrimSpace(after[:end])
		var call struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &call); err == nil && call.Name != "" {
			argsJSON, _ := json.Marshal(call.Arguments)
			results = append(results, llm.ToolCallResult{
				ID:        fmt.Sprintf("text-tc-%d", len(results)),
				Name:      call.Name,
				Arguments: string(argsJSON),
			})
		}
		remaining = after[end+len("</tool_call>"):]
	}
	return results
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

func formatToolResult(toolName string, execution *llm.ExecutionResult, err error) string {
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
