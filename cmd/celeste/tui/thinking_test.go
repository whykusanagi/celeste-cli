package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Reasoning streamed before the reply shows as a thinking count in the
// status bar, never in the chat (L4).
func TestThinkingShowsInTheStatusOnly(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	m, _ = feed(t, m, ThinkingMsg{Delta: strings.Repeat("a", 2000)})
	assert.Contains(t, m.status.text, "thinking", "status: %q", m.status.text)
	assert.Contains(t, m.status.text, "~500 tokens", "status: %q", m.status.text)
	m, _ = feed(t, m, ThinkingMsg{Delta: strings.Repeat("b", 2000)})
	assert.Contains(t, m.status.text, "~1.0k tokens", "status: %q", m.status.text)
	// The tick keeps the count while the spinner animates.
	m, _ = step(t, m, TickMsg{})
	assert.Contains(t, m.status.text, "~1.0k tokens", "status: %q", m.status.text)
	for _, msg := range m.chat.GetMessages() {
		assert.NotContains(t, msg.Content, "aaaa", "reasoning reached the chat")
	}
	// The next request starts its own count.
	m, _ = feed(t, m, TurnStartMsg{Turn: 2})
	assert.NotContains(t, m.status.text, "tokens", "status: %q", m.status.text)
}

// /plan keeps "Planning..." and adds the count: a long plan request on a
// local reasoning model is no longer a silent wait.
func TestThinkingWhilePlanningKeepsPlanningStatus(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m.planning = true
	m.status = m.status.SetText("Planning...")
	m, _ = feed(t, m, ThinkingMsg{Delta: strings.Repeat("a", 400)})
	assert.Equal(t, "Planning... · thinking… ~100 tokens", m.status.text)
}
