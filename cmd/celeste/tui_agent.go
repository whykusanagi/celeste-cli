package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/textutil"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

func agentKindToTUI(k agent.ProgressKind) tui.AgentProgressKind {
	switch k {
	case agent.ProgressTurnStart:
		return tui.AgentProgressTurnStart
	case agent.ProgressToolCall:
		return tui.AgentProgressToolCall
	case agent.ProgressStepDone:
		return tui.AgentProgressStepDone
	case agent.ProgressResponse:
		return tui.AgentProgressResponse
	case agent.ProgressComplete:
		return tui.AgentProgressComplete
	default:
		return tui.AgentProgressError
	}
}

type agentRunnerAPI interface {
	ListRuns(limit int) ([]agent.RunSummary, error)
	Resume(ctx context.Context, runID string) (*agent.RunState, error)
	RunGoal(ctx context.Context, goal string) (*agent.RunState, error)
	// Close stops the runner's MCP servers and code graph; every /agent
	// command closes its runner when it ends.
	Close()
}

var newAgentRunnerForTUI = func(cfg *config.Config, options agent.Options, out io.Writer, errOut io.Writer) (agentRunnerAPI, error) {
	return agent.NewRunner(cfg, options, out, errOut)
}

// listAgentRunsForTUI reads the checkpoint store directly: listing runs
// needs no model, MCP servers or code graph, so /agent list doesn't pay a
// full runner's startup.
var listAgentRunsForTUI = func(limit int) ([]agent.RunSummary, error) {
	store, err := agent.NewCheckpointStore("")
	if err != nil {
		return nil, err
	}
	return store.List(limit)
}

// tuiAgentOptions are the options every /agent runner shares. The TUI
// session already fired SessionStart, so the run is nested, under parent
// (the chat's Env, 2.0 F2e; nil builds the runner its own); setup and hook
// warnings go to the log and the chat, like the session's own hook warnings.
func tuiAgentOptions(parent loop.Nester) agent.Options {
	opts := agent.DefaultOptions()
	if cwd, err := os.Getwd(); err == nil {
		opts.Workspace = cwd
	}
	opts.Verbose = false
	opts.Nested = true
	opts.CheckGoal = true // the goal is the user's (2.0 F2e)
	opts.ParentEnv = parent
	opts.Warn = tuiAgentWarn
	return opts
}

func tuiAgentWarn(s string) {
	tui.LogInfo(s)
	if f := hookNotify.Load(); f != nil {
		(*f)(s)
	}
}

// The chat finds /agent by a type assertion, so a signature drift would
// only show up as "/agent is unavailable"; this makes it a compile error.
var _ tui.AgentCommandRunner = (*TUIClientAdapter)(nil)

// RunAgentCommand dispatches /agent sub-commands.
// Info commands (help, list, resume) return a single AgentCommandResultMsg.
// Goal commands stream incremental AgentProgressMsg via a channel.
func (a *TUIClientAdapter) RunAgentCommand(args []string, run uint64) tea.Cmd {
	if len(args) == 0 {
		return func() tea.Msg {
			return tui.AgentCommandResultMsg{Output: agentUsage(), Err: fmt.Errorf("missing arguments"), AgentRun: run}
		}
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "help", "--help", "-h":
		return func() tea.Msg {
			return tui.AgentCommandResultMsg{Output: agentUsage(), AgentRun: run}
		}
	case "list", "list-runs", "--list-runs":
		copiedArgs := append([]string(nil), args...)
		return func() tea.Msg {
			output, err := a.executeAgentCommand(copiedArgs)
			return tui.AgentCommandResultMsg{Output: output, Err: err, AgentRun: run}
		}
	case "resume", "--resume":
		copiedArgs := append([]string(nil), args...)
		return func() tea.Msg {
			output, err := a.executeAgentCommand(copiedArgs)
			return tui.AgentCommandResultMsg{Output: output, Err: err, AgentRun: run}
		}
	default:
		// Treat all other input as a goal — stream progress.
		return a.runGoalWithProgress(args, run)
	}
}

// runGoalWithProgress runs a goal in a goroutine and streams AgentProgressMsg
// back to the TUI via a bidirectional channel. The read end is stored in each
// non-terminal AgentProgressMsg so app.go can schedule the next read.
func (a *TUIClientAdapter) runGoalWithProgress(args []string, run uint64) tea.Cmd {
	// ch is bidirectional so the goroutine can write and we can hand the
	// receive end (<-chan) to AgentProgressMsg.Ch without a compile error.
	ch := make(chan tui.AgentProgressMsg, 256)

	// The run is cancellable: the TUI stores cancel via StreamStartMsg, so
	// Esc and Ctrl+C stop it (#172).
	ctx, cancel := context.WithCancel(context.Background())
	// Its permission and ask requests name this /agent run (2.0 F2e).
	ctx = tui.WithRunOwner(ctx, tui.RunOwner{Kind: tui.OwnerAgent, Run: run})

	go func() {
		defer close(ch)
		defer cancel()
		cfg := a.currentAgentConfig()
		if cfg.APIKey == "" && needsAPIKey(cfg) {
			sendAgentProgress(ch, tui.AgentProgressMsg{AgentRun: run, Kind: tui.AgentProgressError, Text: "no API key or credentials configured"})
			return
		}

		opts := tuiAgentOptions(a.parentEnv)
		// Tools the permission policy resolves to Ask go through the TUI's
		// permission modal; without this every mutating tool was denied (#172).
		opts.PromptFunc = a.promptFn

		// Capture per-turn timing and token counts.
		// OnTurnStats fires immediately after SendMessageSync returns (before any
		// ProgressToolCall), so stats are always available when ProgressToolCall fires.
		turnStatsMap := make(map[int]agent.TurnStats)
		// turnStatsEmitted[turn] tracks whether we forwarded stats for that turn.
		// We emit on the FIRST ProgressToolCall (tool-call turns) or on
		// ProgressResponse (completion turns with no tool calls).
		turnStatsEmitted := make(map[int]bool)
		opts.OnTurnStats = func(stats agent.TurnStats) {
			agent.KeepTurnStats(turnStatsMap, stats)
		}

		// Pass the receive end of ch so AgentProgressMsg.Ch is a <-chan.
		recvCh := (<-chan tui.AgentProgressMsg)(ch)
		opts.OnProgress = func(kind agent.ProgressKind, text string, turn, maxTurns int) {
			tuiKind := agentKindToTUI(kind)
			var msgCh <-chan tui.AgentProgressMsg
			// Terminal kinds close the chain — don't set Ch so ReadNext returns nil.
			if tuiKind != tui.AgentProgressComplete && tuiKind != tui.AgentProgressError {
				msgCh = recvCh
			}
			msg := tui.AgentProgressMsg{
				AgentRun: run,
				Kind:     tuiKind,
				Text:     text,
				Turn:     turn,
				MaxTurns: maxTurns,
				Ch:       msgCh,
			}
			// Attach per-turn stats to the FIRST ProgressToolCall of each turn.
			// This makes every tool-call turn show timing+tokens immediately,
			// matching the orchestrator runner behaviour.
			if tuiKind == tui.AgentProgressToolCall && !turnStatsEmitted[turn] {
				if stats, ok := turnStatsMap[turn]; ok {
					msg.Duration = stats.Elapsed
					msg.InputTokens = stats.InputTokens
					msg.OutputTokens = stats.OutputTokens
					turnStatsEmitted[turn] = true
					delete(turnStatsMap, turn)
				}
			}
			// For completion turns (no tool calls) attach stats to ProgressResponse.
			if tuiKind == tui.AgentProgressResponse {
				if stats, ok := turnStatsMap[turn]; ok {
					msg.Duration = stats.Elapsed
					msg.InputTokens = stats.InputTokens
					msg.OutputTokens = stats.OutputTokens
					delete(turnStatsMap, turn)
				}
			}
			logAgentProgress(tuiKind, msg)
			sendAgentProgress(ch, msg)
		}

		runner, err := newAgentRunnerForTUI(cfg, opts, io.Discard, io.Discard)
		if err != nil {
			sendAgentProgress(ch, tui.AgentProgressMsg{AgentRun: run, Kind: tui.AgentProgressError, Text: err.Error()})
			return
		}
		defer runner.Close()

		goal := strings.TrimSpace(strings.Join(args, " "))
		state, runErr := runner.RunGoal(ctx, goal)
		if runErr != nil {
			// OnProgress already sent ProgressError via the callback; nothing else needed.
			_ = state
			return
		}
		// Defensive: if the runner didn't fire ProgressComplete via OnProgress
		// (e.g., future runner implementation gap), emit it here so the TUI
		// always receives a terminal event and stops streaming.
		lastResponse := ""
		if state != nil {
			lastResponse = state.LastAssistantResponse
		}
		sendAgentProgress(ch, tui.AgentProgressMsg{AgentRun: run, Kind: tui.AgentProgressComplete, Text: lastResponse})
	}()

	return tea.Batch(
		func() tea.Msg { return tui.StreamStartMsg{Cancel: cancel, AgentRun: run} },
		func() tea.Msg {
			msg, ok := <-ch
			if !ok {
				return nil
			}
			return msg
		},
	)
}

// agentTerminalSendTimeout bounds how long the run waits to deliver its final
// message if the TUI has stopped reading.
const agentTerminalSendTimeout = 5 * time.Second

// sendAgentProgress delivers a progress message without letting a stalled UI
// block the agent (#172). Intermediate messages are dropped when the buffer
// is full; the terminal message, which ends the TUI's read chain, waits a
// bounded time for room.
func sendAgentProgress(ch chan<- tui.AgentProgressMsg, msg tui.AgentProgressMsg) {
	if msg.Kind != tui.AgentProgressComplete && msg.Kind != tui.AgentProgressError {
		select {
		case ch <- msg:
		default:
		}
		return
	}
	select {
	case ch <- msg:
	case <-time.After(agentTerminalSendTimeout):
	}
}

func (a *TUIClientAdapter) executeAgentCommand(args []string) (string, error) {
	if len(args) == 0 {
		return agentUsage(), fmt.Errorf("missing agent command arguments")
	}

	cfg := a.currentAgentConfig()
	if cfg.APIKey == "" && needsAPIKey(cfg) {
		return "", fmt.Errorf("no API key or Google credentials configured for agent execution")
	}

	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "help", "--help", "-h":
		return agentUsage(), nil
	case "list", "list-runs", "--list-runs":
		runs, err := listAgentRunsForTUI(20)
		if err != nil {
			return "", fmt.Errorf("list runs: %w", err)
		}
		return formatAgentRunList(runs), nil
	}
	// Resume runs the agent again, so it needs a full runner (model client,
	// tools, MCP, hooks), as does a goal.
	runner, err := newAgentRunnerForTUI(cfg, tuiAgentOptions(a.parentEnv), io.Discard, io.Discard)
	if err != nil {
		return "", fmt.Errorf("create agent runner: %w", err)
	}
	defer runner.Close()
	ctx := context.Background()

	switch sub {
	case "resume", "--resume":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return agentUsage(), fmt.Errorf("usage: /agent resume <run-id>")
		}
		state, runErr := runner.Resume(ctx, strings.TrimSpace(args[1]))
		output := formatAgentRunSummary(state)
		if runErr != nil {
			return output, fmt.Errorf("resume failed: %w", runErr)
		}
		if state != nil && state.Status != agent.StatusCompleted {
			return output, fmt.Errorf("agent resumed with status %s", state.Status)
		}
		return output, nil
	case "goal", "run", "--goal":
		goal := strings.TrimSpace(strings.Join(args[1:], " "))
		if goal == "" {
			return agentUsage(), fmt.Errorf("usage: /agent %s <goal>", sub)
		}
		return runAgentGoal(ctx, runner, goal)
	default:
		goal := strings.TrimSpace(strings.Join(args, " "))
		return runAgentGoal(ctx, runner, goal)
	}
}

func runAgentGoal(ctx context.Context, runner agentRunnerAPI, goal string) (string, error) {
	state, runErr := runner.RunGoal(ctx, goal)
	output := formatAgentRunSummary(state)
	if runErr != nil {
		return output, fmt.Errorf("agent failed: %w", runErr)
	}
	if state != nil && state.Status != agent.StatusCompleted {
		return output, fmt.Errorf("agent finished with status %s", state.Status)
	}
	return output, nil
}

func (a *TUIClientAdapter) currentAgentConfig() *config.Config {
	var cfg config.Config
	if a.baseConfig != nil {
		cfg = *a.baseConfig
	} else {
		cfg = *config.DefaultConfig()
	}

	if a.client != nil && a.client.GetConfig() != nil {
		current := a.client.GetConfig()
		cfg.APIKey = current.APIKey
		cfg.BaseURL = current.BaseURL
		cfg.Model = current.Model
		cfg.SimulateTyping = current.SimulateTyping
		cfg.TypingSpeed = current.TypingSpeed
		cfg.GoogleCredentialsFile = current.GoogleCredentialsFile
		cfg.GoogleUseADC = current.GoogleUseADC
		cfg.Collections = current.Collections
		cfg.XAIFeatures = current.XAIFeatures
		if current.Timeout > 0 {
			cfg.Timeout = int(current.Timeout / time.Second)
		}
	}

	if cfg.MaxToolIterations <= 0 {
		cfg.MaxToolIterations = config.DefaultMaxToolIterations
	}

	return &cfg
}

func agentUsage() string {
	return "Usage: /agent <goal>\n       /agent list-runs\n       /agent resume <run-id>"
}

func formatAgentRunList(runs []agent.RunSummary) string {
	if len(runs) == 0 {
		return "No agent runs found."
	}

	lines := []string{fmt.Sprintf("Recent Agent Runs (%d):", len(runs))}
	for _, r := range runs {
		goalPreview := strings.TrimSpace(r.Goal)
		if len(goalPreview) > 72 {
			goalPreview = goalPreview[:72] + "..."
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] turns=%d tools=%d updated=%s", r.RunID, r.Status, r.Turn, r.ToolCalls, r.UpdatedAt.Format("2006-01-02 15:04:05")))
		lines = append(lines, fmt.Sprintf("  goal: %s", goalPreview))
	}
	return strings.Join(lines, "\n")
}

func formatAgentRunSummary(state *agent.RunState) string {
	if state == nil {
		return "Agent run completed with no state payload."
	}

	lines := []string{
		fmt.Sprintf("Run ID: %s", state.RunID),
		fmt.Sprintf("Status: %s", state.Status),
		fmt.Sprintf("Turns: %d", state.Turn),
		fmt.Sprintf("Tool Calls: %d", state.ToolCallCount),
	}

	if strings.TrimSpace(state.ArtifactBundlePath) != "" {
		lines = append(lines, fmt.Sprintf("Artifacts: %s", state.ArtifactBundlePath))
	}
	if strings.TrimSpace(state.Error) != "" {
		lines = append(lines, fmt.Sprintf("Error: %s", state.Error))
	}
	if strings.TrimSpace(state.LastAssistantResponse) != "" {
		lines = append(lines, "", "Final Response:", previewText(state.LastAssistantResponse, 1800))
	}

	return strings.Join(lines, "\n")
}

func previewText(value string, limit int) string {
	text := strings.TrimSpace(value)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return textutil.CutBytes(text, limit) + "\n...(truncated)"
}

// logAgentProgress writes agent progress events to the session log so they
// appear alongside orchestrator events in /export logs.
func logAgentProgress(kind tui.AgentProgressKind, msg tui.AgentProgressMsg) {
	switch kind {
	case tui.AgentProgressTurnStart:
		tui.LogInfo(fmt.Sprintf("[AGENT] turn %d/%d start", msg.Turn, msg.MaxTurns))
	case tui.AgentProgressToolCall:
		line := fmt.Sprintf("[AGENT] turn %d tool=%s", msg.Turn, msg.Text)
		if msg.InputTokens > 0 || msg.Duration > 0 {
			line += fmt.Sprintf(" elapsed=%.2fs tokens=↑%d ↓%d",
				msg.Duration.Seconds(), msg.InputTokens, msg.OutputTokens)
		}
		tui.LogInfo(line)
	case tui.AgentProgressStepDone:
		tui.LogInfo(fmt.Sprintf("[AGENT] step done: %s", msg.Text))
	case tui.AgentProgressResponse:
		line := fmt.Sprintf("[AGENT] turn %d response", msg.Turn)
		if msg.InputTokens > 0 || msg.Duration > 0 {
			line += fmt.Sprintf(" elapsed=%.2fs tokens=↑%d ↓%d",
				msg.Duration.Seconds(), msg.InputTokens, msg.OutputTokens)
		}
		tui.LogInfo(line)
		if strings.TrimSpace(msg.Text) != "" {
			tui.LogInfo(fmt.Sprintf("[AGENT] response:\n%s", msg.Text))
		}
	case tui.AgentProgressComplete:
		tui.LogInfo("[AGENT] run complete")
	case tui.AgentProgressError:
		tui.LogInfo(fmt.Sprintf("[AGENT] error: %s", msg.Text))
	}
}
