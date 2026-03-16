package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// logOrchestratorEvent writes a full trace of every orchestrator event to the session log.
func logOrchestratorEvent(e orchestrator.OrchestratorEvent) {
	kindName := []string{
		"CLASSIFIED", "ACTION", "TOOL_CALL", "FILE_DIFF",
		"REVIEW_DRAFT", "DEFENSE", "VERDICT", "COMPLETE", "ERROR", "DEBATE_START",
	}
	name := fmt.Sprintf("KIND_%d", int(e.Kind))
	if int(e.Kind) < len(kindName) {
		name = kindName[int(e.Kind)]
	}

	// Base line with event kind, model, lane
	line := fmt.Sprintf("[ORCH] %s", name)
	if e.Model != "" {
		line += fmt.Sprintf(" model=%s", e.Model)
	}
	if string(e.Lane) != "" {
		line += fmt.Sprintf(" lane=%s", e.Lane)
	}

	// Timing and tokens on every event that carries them
	if e.Duration > 0 {
		line += fmt.Sprintf(" elapsed=%.2fs", e.Duration.Seconds())
	}
	if e.InputTokens > 0 || e.OutputTokens > 0 {
		line += fmt.Sprintf(" tokens=↑%d ↓%d", e.InputTokens, e.OutputTokens)
	}
	if e.Score > 0 {
		line += fmt.Sprintf(" score=%.2f", e.Score)
	}
	tui.LogInfo(line)

	// Summary text on a second line
	if e.Text != "" {
		tui.LogInfo(fmt.Sprintf("[ORCH] %s text: %s", name, e.Text))
	}

	// Full response content (agent turn output, reviewer critique, defense)
	if e.Response != "" {
		tui.LogInfo(fmt.Sprintf("[ORCH] %s response:\n%s", name, e.Response))
	}

	// File path for diffs
	if e.FilePath != "" {
		tui.LogInfo(fmt.Sprintf("[ORCH] %s file: %s", name, e.FilePath))
	}
}

// RunOrchestratorCommand launches an orchestrated agent run from the TUI.
// Returns a tea.Cmd that streams OrchestratorEventMsg to the TUI.
func (a *TUIClientAdapter) RunOrchestratorCommand(goal string) tea.Cmd {
	// Buffer=1: allows the goroutine to be at most one event ahead of the TUI reader.
	// This creates backpressure so events stream in real-time rather than all appearing
	// at once after the agent run completes (what happens with a large buffer).
	ch := make(chan tui.OrchestratorEventMsg, 1)

	go func() {
		defer close(ch)
		cfg := a.currentAgentConfig()
		o := orchestrator.New(cfg)
		tui.LogInfo(fmt.Sprintf("[ORCH] run started goal=%q", goal))
		// recvCh is the receive end of ch; needed because OrchestratorEventMsg.Ch
		// is <-chan (receive-only) but ch is bidirectional.
		recvCh := (<-chan tui.OrchestratorEventMsg)(ch)
		o.OnEvent(func(e orchestrator.OrchestratorEvent) {
			logOrchestratorEvent(e)
			var msgCh <-chan tui.OrchestratorEventMsg
			if e.Kind != orchestrator.EventComplete && e.Kind != orchestrator.EventError {
				msgCh = recvCh
			}
			ch <- tui.OrchestratorEventMsg{
				Kind:         int(e.Kind),
				Lane:         string(e.Lane),
				Text:         e.Text,
				Model:        e.Model,
				Duration:     e.Duration,
				InputTokens:  e.InputTokens,
				OutputTokens: e.OutputTokens,
				Response:     e.Response,
				FilePath:     e.FilePath,
				Diff:         e.Diff,
				Score:        e.Score,
				Ch:           msgCh,
			}
		})
		// Run emits EventComplete or EventError via OnEvent before returning.
		_, _ = o.Run(context.Background(), goal)
		tui.LogInfo("[ORCH] run finished")
	}()

	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}
