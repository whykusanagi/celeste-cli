package main

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// RunOrchestratorCommand launches an orchestrated agent run from the TUI.
// Returns a tea.Cmd that streams OrchestratorEventMsg to the TUI.
func (a *TUIClientAdapter) RunOrchestratorCommand(goal string) tea.Cmd {
	ch := make(chan tui.OrchestratorEventMsg, 64)

	go func() {
		defer close(ch)
		cfg := a.currentAgentConfig()
		o := orchestrator.New(cfg)
		// recvCh is the receive end of ch; needed because OrchestratorEventMsg.Ch
		// is <-chan (receive-only) but ch is bidirectional.
		recvCh := (<-chan tui.OrchestratorEventMsg)(ch)
		o.OnEvent(func(e orchestrator.OrchestratorEvent) {
			var msgCh <-chan tui.OrchestratorEventMsg
			if e.Kind != orchestrator.EventComplete && e.Kind != orchestrator.EventError {
				msgCh = recvCh
			}
			ch <- tui.OrchestratorEventMsg{
				Kind:     int(e.Kind),
				Lane:     string(e.Lane),
				Text:     e.Text,
				FilePath: e.FilePath,
				Diff:     e.Diff,
				Score:    e.Score,
				Ch:       msgCh,
			}
		})
		// Run emits EventComplete or EventError via OnEvent before returning.
		_, _ = o.Run(context.Background(), goal)
	}()

	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}
