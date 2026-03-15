package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
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
}

var newAgentRunnerForTUI = func(cfg *config.Config, options agent.Options, out io.Writer, errOut io.Writer) (agentRunnerAPI, error) {
	return agent.NewRunner(cfg, options, out, errOut)
}

// RunAgentCommand dispatches /agent sub-commands.
// Info commands (help, list, resume) return a single AgentCommandResultMsg.
// Goal commands stream incremental AgentProgressMsg via a channel.
func (a *TUIClientAdapter) RunAgentCommand(args []string) tea.Cmd {
	if len(args) == 0 {
		return func() tea.Msg {
			return tui.AgentCommandResultMsg{Output: agentUsage(), Err: fmt.Errorf("missing arguments")}
		}
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "help", "--help", "-h":
		return func() tea.Msg {
			return tui.AgentCommandResultMsg{Output: agentUsage()}
		}
	case "list", "list-runs", "--list-runs":
		copiedArgs := append([]string(nil), args...)
		return func() tea.Msg {
			output, err := a.executeAgentCommand(copiedArgs)
			return tui.AgentCommandResultMsg{Output: output, Err: err}
		}
	case "resume", "--resume":
		copiedArgs := append([]string(nil), args...)
		return func() tea.Msg {
			output, err := a.executeAgentCommand(copiedArgs)
			return tui.AgentCommandResultMsg{Output: output, Err: err}
		}
	default:
		// Treat all other input as a goal — stream progress.
		return a.runGoalWithProgress(args)
	}
}

// runGoalWithProgress runs a goal in a goroutine and streams AgentProgressMsg
// back to the TUI via a bidirectional channel. The read end is stored in each
// non-terminal AgentProgressMsg so app.go can schedule the next read.
func (a *TUIClientAdapter) runGoalWithProgress(args []string) tea.Cmd {
	// ch is bidirectional so the goroutine can write and we can hand the
	// receive end (<-chan) to AgentProgressMsg.Ch without a compile error.
	ch := make(chan tui.AgentProgressMsg, 64)

	go func() {
		defer close(ch)
		cfg := a.currentAgentConfig()
		if cfg.APIKey == "" && !cfg.GoogleUseADC && strings.TrimSpace(cfg.GoogleCredentialsFile) == "" {
			ch <- tui.AgentProgressMsg{Kind: tui.AgentProgressError, Text: "no API key or credentials configured"}
			return
		}

		opts := agent.DefaultOptions()
		if cwd, err := os.Getwd(); err == nil {
			opts.Workspace = cwd
		}
		opts.Verbose = false
		// Pass the receive end of ch so AgentProgressMsg.Ch is a <-chan.
		recvCh := (<-chan tui.AgentProgressMsg)(ch)
		opts.OnProgress = func(kind agent.ProgressKind, text string, turn, maxTurns int) {
			tuiKind := agentKindToTUI(kind)
			var msgCh <-chan tui.AgentProgressMsg
			// Terminal kinds close the chain — don't set Ch so ReadNext returns nil.
			if tuiKind != tui.AgentProgressComplete && tuiKind != tui.AgentProgressError {
				msgCh = recvCh
			}
			ch <- tui.AgentProgressMsg{
				Kind:     tuiKind,
				Text:     text,
				Turn:     turn,
				MaxTurns: maxTurns,
				Ch:       msgCh,
			}
		}

		runner, err := newAgentRunnerForTUI(cfg, opts, io.Discard, io.Discard)
		if err != nil {
			ch <- tui.AgentProgressMsg{Kind: tui.AgentProgressError, Text: err.Error()}
			return
		}

		goal := strings.TrimSpace(strings.Join(args, " "))
		state, runErr := runner.RunGoal(context.Background(), goal)
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
		ch <- tui.AgentProgressMsg{Kind: tui.AgentProgressComplete, Text: lastResponse}
	}()

	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (a *TUIClientAdapter) executeAgentCommand(args []string) (string, error) {
	if len(args) == 0 {
		return agentUsage(), fmt.Errorf("missing agent command arguments")
	}

	cfg := a.currentAgentConfig()
	if cfg.APIKey == "" && !cfg.GoogleUseADC && strings.TrimSpace(cfg.GoogleCredentialsFile) == "" {
		return "", fmt.Errorf("no API key or Google credentials configured for agent execution")
	}

	opts := agent.DefaultOptions()
	if cwd, err := os.Getwd(); err == nil {
		opts.Workspace = cwd
	}
	opts.Verbose = false

	runner, err := newAgentRunnerForTUI(cfg, opts, io.Discard, io.Discard)
	if err != nil {
		return "", fmt.Errorf("create agent runner: %w", err)
	}

	sub := strings.ToLower(strings.TrimSpace(args[0]))
	ctx := context.Background()

	switch sub {
	case "help", "--help", "-h":
		return agentUsage(), nil
	case "list", "list-runs", "--list-runs":
		runs, err := runner.ListRuns(20)
		if err != nil {
			return "", fmt.Errorf("list runs: %w", err)
		}
		return formatAgentRunList(runs), nil
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
		cfg.SkipPersonaPrompt = current.SkipPersonaPrompt
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

	cfg.RuntimeMode = config.NormalizeRuntimeMode(cfg.RuntimeMode)
	if cfg.ClawMaxToolIterations <= 0 {
		cfg.ClawMaxToolIterations = config.DefaultClawMaxToolIterations
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
	return text[:limit] + "\n...(truncated)"
}
