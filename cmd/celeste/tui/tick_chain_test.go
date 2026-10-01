package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tickClock plays the part of Bubble Tea's timer for one AppModel: it keeps
// the TickMsgs the model has scheduled and not yet received, and delivers
// them oldest first.
type tickClock struct {
	t       *testing.T
	m       AppModel
	pending []TickMsg
	max     int
}

// take runs cmd's tree and keeps the TickMsgs it schedules.
func (c *tickClock) take(cmd tea.Cmd) {
	for _, msg := range collectMsgs(cmd) {
		if tk, ok := msg.(TickMsg); ok {
			c.pending = append(c.pending, tk)
		}
	}
	if len(c.pending) > c.max {
		c.max = len(c.pending)
	}
}

func (c *tickClock) send(msg tea.Msg) {
	c.t.Helper()
	var cmd tea.Cmd
	c.m, cmd = step(c.t, c.m, msg)
	c.take(cmd)
}

func (c *tickClock) feed(msg tea.Msg) {
	c.t.Helper()
	c.send(TurnEventMsg{Run: c.m.turnRun, Msg: msg})
}

// tick delivers the oldest pending TickMsg, if any.
func (c *tickClock) tick() {
	c.t.Helper()
	if len(c.pending) == 0 {
		return
	}
	tk := c.pending[0]
	c.pending = c.pending[1:]
	c.send(tk)
}

// At most one tick chain runs however many tool turns a reply takes, so
// the spinner and the typing keep their speed in long tool turns. Before
// 2.0 F2e every tool turn added chains (TurnStartMsg after tools, the
// first text chunk, the first tool start).
func TestOneTickChainAcrossToolTurns(t *testing.T) {
	m, _ := newQueueTestApp()
	c := &tickClock{t: t, m: m}
	c.send(SendMessageMsg{Content: "go"})
	require.NotNil(t, c.m.turn)
	for i, id := range []string{"call_a", "call_b", "call_c", "call_d"} {
		c.feed(TurnStartMsg{Turn: i + 1})
		c.tick()
		c.feed(StreamChunkMsg{Chunk: StreamChunk{Content: "working on it", IsFirst: true}})
		c.tick()
		c.feed(ToolTurnMsg{Text: "working on it"})
		c.feed(ToolStartMsg{ID: id, Name: "tool_a"})
		c.tick()
		c.feed(ToolResultMsg{ID: id, Name: "tool_a", Content: `{"ok":true}`})
		c.tick()
		assert.LessOrEqual(t, len(c.pending), 1, "tool turn %d: %d tick chains running", i+1, len(c.pending))
	}
	c.feed(TurnStartMsg{Turn: 5})
	c.feed(StreamChunkMsg{Chunk: StreamChunk{Content: "done", IsFirst: true}})
	c.feed(StreamDoneMsg{FullContent: "done"})
	c.feed(TurnDoneMsg{Stop: "done"})
	for i := 0; i < 10 && len(c.pending) > 0; i++ {
		c.tick()
	}
	assert.Equal(t, 1, c.max, "more than one tick chain ran at once")
	assert.Empty(t, c.pending, "the chain did not end once the reply was typed")
	assert.Empty(t, c.m.typingContent, "the reply was never committed")
	assert.False(t, c.m.streaming)
}

// A sub-view that eats the chain's tick (the menu here) must not leave the
// chain marked pending: the next schedule starts a new one.
func TestTickEatenBySubViewDoesNotStallTheChain(t *testing.T) {
	m, _ := newQueueTestApp()
	c := &tickClock{t: t, m: m}
	c.send(SendMessageMsg{Content: "go"})
	require.Len(t, c.pending, 1)
	menu := NewMenuModel()
	c.m.menuModel = &menu
	c.m.viewMode = "menu"
	c.tick() // eaten by the menu
	assert.False(t, c.m.tickPending, "an eaten tick left the chain pending")
	c.m.viewMode = "chat"
	c.feed(StreamChunkMsg{Chunk: StreamChunk{Content: "hi", IsFirst: true}})
	assert.Len(t, c.pending, 1, "typing started no tick after the menu ate the last one")
}

// A run started while a chain's tick is still out (Enter, /agent, /orch)
// starts a new chain; the old tick does nothing when it arrives.
func TestSupersededTickIsDropped(t *testing.T) {
	m, _ := newQueueTestApp()
	c := &tickClock{t: t, m: m}
	cmd := c.m.tick(typingTickInterval * 2)
	c.take(cmd)
	require.Len(t, c.pending, 1)
	stale := c.pending[0]
	c.pending = nil
	c.send(SendMessageMsg{Content: "go"})
	require.Len(t, c.pending, 1, "Enter did not start a new chain over a pending one")
	frame := c.m.animFrame
	c.send(stale)
	assert.Equal(t, frame, c.m.animFrame, "a superseded tick animated")
	assert.Len(t, c.pending, 1, "a superseded tick scheduled another")
}
