package compact

import (
	"encoding/json"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// Meter tracks, for one compactor (a chat turn, an agent run, an MCP
// call), how big the next request will be and which trailing messages the
// model has not been shown yet (#234). Not safe for concurrent use: the
// loop calls its Compactor from Run's goroutine only.
type Meter struct {
	// Overhead is the system prompt and tool schemas, in estimated tokens.
	Overhead int

	sent    int // history length the last answered request carried; -1: all seen
	pending int // history length handed to the latest request; -1: none yet
	// surplus is how far the provider's prompt count for the last request
	// that reported one exceeded the history estimate of what it carried
	// (system prompt, tool schemas, tokenizer drift). It is kept as a
	// difference, not an absolute count, so a prune or summary lowers Used
	// before the provider reports again.
	surplus int
}

// NewMeter returns a meter for a compactor whose requests carry overhead
// tokens besides the history.
func NewMeter(overhead int) *Meter {
	return &Meter{Overhead: overhead, sent: -1, pending: -1}
}

// Observe is called first in every Compact, with the history about to be
// compacted and the provider's prompt tokens for the previous request (0
// when it reported none). The previous request was answered when its
// reply, an assistant message, sits right after what it carried. A reply
// without a prompt count marks its request as seen but keeps the surplus
// the provider last reported.
func (m *Meter) Observe(history []tui.ChatMessage, promptTokens int) {
	if m.pending >= 0 && m.pending < len(history) && history[m.pending].Role == "assistant" {
		m.sent = m.pending
		if promptTokens > 0 {
			m.surplus = promptTokens - Estimate(history[:m.pending])
		}
	}
}

// Sending is called last in every Compact, with the history the loop will
// send.
func (m *Meter) Sending(history []tui.ChatMessage) { m.pending = len(history) }

// Unseen is how many trailing messages came after the last answered
// request; 0 before the first one.
func (m *Meter) Unseen(history []tui.ChatMessage) int {
	if m.sent < 0 || m.sent > len(history) {
		return 0
	}
	return len(history) - m.sent
}

// Used estimates the next request: the history estimate plus Overhead, or
// plus the provider's last reported surplus over the estimate, whichever
// is larger. Without a prune this equals the provider's count plus what
// was appended after the request it measured; after a prune or summary it
// falls with the history.
func (m *Meter) Used(history []tui.ChatMessage) int {
	return Estimate(history) + max(m.Overhead, m.surplus)
}

// DefinitionTokens estimates the tool schemas a request carries.
func DefinitionTokens(defs []tui.SkillDefinition) int {
	if len(defs) == 0 {
		return 0
	}
	b, err := json.Marshal(defs)
	if err != nil {
		return 0
	}
	return len(b) / 4
}
