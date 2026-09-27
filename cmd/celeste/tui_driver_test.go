package main

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// drive feeds msgs into m, then keeps executing the commands Update returns
// (flattening tea.BatchMsg) until until(m) is true or the timeout expires.
// It replaces tea.Program for tests: no terminal, deterministic order.
func drive(t *testing.T, m tea.Model, msgs []tea.Msg, until func(tea.Model) bool, timeout time.Duration) tea.Model {
	t.Helper()
	queue := append([]tea.Msg(nil), msgs...)
	deadline := time.Now().Add(timeout)
	results := make(chan tea.Msg, 256)
	pending := 0
	run := func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		pending++
		go func() { results <- cmd() }()
	}
	for time.Now().Before(deadline) {
		for len(queue) > 0 {
			msg := queue[0]
			queue = queue[1:]
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					run(c)
				}
				continue
			}
			var cmd tea.Cmd
			m, cmd = m.Update(msg)
			run(cmd)
			if until(m) {
				return m
			}
		}
		if pending == 0 {
			if until(m) {
				return m
			}
			t.Fatalf("drive: no pending work and condition not met")
		}
		select {
		case msg := <-results:
			pending--
			if msg != nil && !isTestDriverTick(msg) {
				queue = append(queue, msg)
			}
		case <-time.After(time.Until(deadline)):
		}
	}
	t.Fatalf("drive: condition not met within %v", timeout)
	return m
}

// isTestDriverTick reports messages the driver must not re-queue.
//
// tui.TickMsg (cmd/celeste/tui/app.go) drives the typewriter reveal of the
// assistant reply (m.typingContent/m.typingPos) and is what eventually flips
// turnActive() back to false — dropping it, as an earlier version of this
// driver did, stranded every characterization test at "no pending work and
// condition not met" because the reply was streamed but never "typed out".
// Production already bounds this: app.go only reschedules another TickMsg
// while typingContent is non-empty or a tool-progress spinner is active
// (guarded explicitly against exponential growth, see the comment above the
// tea.Batch(cmds...) return in (AppModel).update), so letting it through is
// safe and self-terminating for these scripted, short-lived turns.
//
// tui.TypingTickMsg (cmd/celeste/tui/streaming.go) is a second, unrelated
// animation-tick type. Nothing in production actually consumes it (no `case
// TypingTickMsg` anywhere outside this test file as of this writing), so it
// can never legitimately arrive here — it is filtered defensively in case a
// future SimulatedTyping caller wires it up as another self-rescheduling
// loop the driver would otherwise spin on forever.
func isTestDriverTick(msg tea.Msg) bool {
	switch msg.(type) {
	case tui.TypingTickMsg:
		return true
	default:
		return false
	}
}
