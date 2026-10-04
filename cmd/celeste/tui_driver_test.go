package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// drive feeds msgs into m, then keeps executing the commands Update returns
// (flattening tea.BatchMsg) until until(m) is true or the timeout expires.
// It replaces tea.Program for tests: no terminal, deterministic order.
func drive(t *testing.T, m tea.Model, msgs []tea.Msg, until func(tea.Model) bool, timeout time.Duration) tea.Model {
	t.Helper()
	d := newTUIDriver(t, m)
	d.Send(msgs...)
	return d.RunUntil(until, timeout)
}

type tuiTestDriver struct {
	t       *testing.T
	m       tea.Model
	queue   []tea.Msg
	results chan tea.Msg
	pending int
	// external carries messages from outside the driven commands, the way
	// p.Send does for a tea.Program (the permission prompt's bridge).
	external chan tea.Msg
}

func newTUIDriver(t *testing.T, m tea.Model) *tuiTestDriver {
	t.Helper()
	return &tuiTestDriver{
		t:        t,
		m:        m,
		results:  make(chan tea.Msg, 256),
		external: make(chan tea.Msg, 16),
	}
}

func (d *tuiTestDriver) Send(msgs ...tea.Msg) {
	d.queue = append(d.queue, msgs...)
}

func (d *tuiTestDriver) RunUntil(until func(tea.Model) bool, timeout time.Duration) tea.Model {
	d.t.Helper()
	deadline := time.Now().Add(timeout)
	run := func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		d.pending++
		go func() { d.results <- cmd() }()
	}
	for time.Now().Before(deadline) {
		for len(d.queue) > 0 {
			msg := d.queue[0]
			d.queue = d.queue[1:]
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					run(c)
				}
				continue
			}
			var cmd tea.Cmd
			d.m, cmd = d.m.Update(msg)
			run(cmd)
			if until(d.m) {
				return d.m
			}
		}
		if d.pending == 0 {
			if until(d.m) {
				return d.m
			}
			d.t.Fatalf("drive: no pending work and condition not met%s", d.chatTail())
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		select {
		case msg := <-d.results:
			d.pending--
			if msg != nil {
				d.queue = append(d.queue, msg)
			}
		case msg := <-d.external:
			d.queue = append(d.queue, msg)
		case <-time.After(remaining):
		}
	}
	d.t.Fatalf("drive: condition not met within %v%s", timeout, d.chatTail())
	return d.m
}

// chatTail is the end of the driven chat, for a failure message: the
// system line a slash command answered with is usually the reason its
// condition never held (#327 only said "no pending work").
func (d *tuiTestDriver) chatTail() string {
	am, ok := d.m.(tui.AppModel)
	if !ok {
		return ""
	}
	msgs := am.DebugMessages()
	if len(msgs) > 6 {
		msgs = msgs[len(msgs)-6:]
	}
	var b strings.Builder
	b.WriteString("; last chat messages:")
	for _, m := range msgs {
		c := m.Content
		if len(c) > 300 {
			c = c[:300] + "…"
		}
		fmt.Fprintf(&b, "\n  %s: %q", m.Role, c)
	}
	return b.String()
}

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
