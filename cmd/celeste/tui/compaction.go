package tui

import (
	"fmt"
)

// compactContext prunes old tool results when the history is over the
// compaction threshold, or unconditionally when force is set (/context
// compact, or after a context-overflow error). It reports whether anything
// was pruned.
func (m AppModel) compactContext(force bool) (AppModel, bool) {
	c, ok := m.llmClient.(ContextCompactor)
	if !ok || m.contextTracker == nil || m.contextTracker.MaxTokens <= 0 {
		return m, false
	}
	msgs := m.chat.GetLLMMessages()
	// The tracker's count comes from the API and includes the system prompt
	// and tool schemas; the compactor adds its own estimate of the history.
	edits, summary, saved := c.CompactContext(msgs, m.contextTracker.MaxTokens, m.contextTracker.CurrentTokens, force)
	if len(edits) == 0 {
		return m, false
	}
	m.chat = m.chat.ReplaceToolResults(edits)
	m.contextTracker.CompactionCount++
	if m.contextTracker.CurrentTokens > saved {
		m.contextTracker.CurrentTokens -= saved
	}
	m.header = m.header.SetContextUsage(m.contextTracker.CurrentTokens, m.contextTracker.MaxTokens)
	m.chat = m.chat.AddSystemMessage(fmt.Sprintf("🗜 Context compacted: %s", summary))
	LogInfo("context compacted: " + summary)
	return m, true
}
