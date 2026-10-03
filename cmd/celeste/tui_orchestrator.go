package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/orchestrator"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
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

// orchestratorEventSender is /orch's event callback: it logs every event
// and forwards it to ch, handing the TUI recv (ch's receive end) to read the
// next one from, until the terminal event (EventComplete or EventError).
// After that the TUI stops reading (the terminal message's Ch is nil), so
// anything later is logged and dropped, never sent: a send would block on a
// channel nobody reads, or panic once it is closed. The orchestrator
// delivers events one at a time, so finished needs no lock.
func orchestratorEventSender(ch chan<- tui.OrchestratorEventMsg, recv <-chan tui.OrchestratorEventMsg, run uint64) func(orchestrator.OrchestratorEvent) {
	finished := false
	return func(e orchestrator.OrchestratorEvent) {
		logOrchestratorEvent(e)
		if finished {
			return
		}
		terminal := e.Kind == orchestrator.EventComplete || e.Kind == orchestrator.EventError
		next := recv
		if terminal {
			next = nil
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
			Ch:           next,
			Run:          run,
		}
		finished = terminal
	}
}

// RunOrchestratorCommand launches an orchestrated agent run from the TUI.
// Returns a tea.Cmd that streams OrchestratorEventMsg to the TUI. The run is
// cancellable: the TUI stores its cancel via StreamStartMsg, so Esc and
// Ctrl+C stop it, as they do /agent. run tags the StreamStartMsg and every
// event, so the TUI can ignore a run it already cancelled.
func (a *TUIClientAdapter) RunOrchestratorCommand(goal string, run uint64) tea.Cmd {
	// Buffer=1: allows the goroutine to be at most one event ahead of the TUI reader.
	// This creates backpressure so events stream in real-time rather than all appearing
	// at once after the agent run completes (what happens with a large buffer).
	ch := make(chan tui.OrchestratorEventMsg, 1)
	ctx, cancel := context.WithCancel(context.Background())
	// Its lanes' permission requests name this run (2.0 F2e).
	ctx = tui.WithRunOwner(ctx, tui.RunOwner{Kind: tui.OwnerOrch, Run: run})

	go func() {
		defer close(ch)
		defer cancel()
		cfg := a.currentAgentConfig()
		// Lanes ask the chat's permission modal, as /agent does (#172);
		// before 2.0 every mutating tool was silently denied (spec §3.2).
		o := orchestrator.New(cfg, orchestrator.WithPrompt(a.promptFn))
		tui.LogInfo(fmt.Sprintf("[ORCH] run started goal=%q", goal))
		o.OnEvent(orchestratorEventSender(ch, ch, run))
		// Run emits EventComplete or EventError via OnEvent before returning.
		_, _ = o.Run(ctx, goal)
		tui.LogInfo("[ORCH] run finished")
	}()

	return tea.Batch(
		func() tea.Msg { return tui.StreamStartMsg{Cancel: cancel, Run: run} },
		func() tea.Msg {
			msg, ok := <-ch
			if !ok {
				return nil
			}
			return msg
		},
	)
}
