package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// summaryTimeout bounds a compaction summary request.
const summaryTimeout = 3 * time.Minute

// ContextSummarizedMsg delivers a compaction summary written in the
// background (#174).
type ContextSummarizedMsg struct {
	Outcome SummaryOutcome
	Err     error
	// snapshotLen and firstContent identify the history the summary was
	// written from, so a stale summary (after /clear or a session switch)
	// is dropped rather than applied to a different conversation.
	snapshotLen  int
	firstContent string
	manual       bool
}

// compactContext prunes old tool results when the history is over the
// compaction threshold, or unconditionally when force is set (/context
// compact, or after a context-overflow error).
func (m AppModel) compactContext(force bool) (AppModel, CompactOutcome) {
	c, ok := m.llmClient.(ContextCompactor)
	if !ok || m.contextTracker == nil || m.contextTracker.MaxTokens <= 0 {
		return m, CompactOutcome{}
	}
	msgs := m.chat.GetLLMMessages()
	// The tracker's count comes from the API and includes the system prompt
	// and tool schemas; the compactor adds its own estimate of the history.
	out := c.CompactContext(msgs, m.contextTracker.MaxTokens, m.contextTracker.CurrentTokens, force)
	if len(out.Edits) == 0 {
		return m, out
	}
	m.chat = m.chat.ReplaceToolResults(out.Edits)
	m.contextTracker.CompactionCount++
	if m.contextTracker.CurrentTokens > out.SavedTokens {
		m.contextTracker.CurrentTokens -= out.SavedTokens
	}
	m.header = m.header.SetContextUsage(m.contextTracker.CurrentTokens, m.contextTracker.MaxTokens)
	m.chat = m.chat.AddSystemMessage(fmt.Sprintf("🗜 Context compacted: %s", out.Summary))
	LogInfo("context compacted: " + out.Summary)
	return m, out
}

// startSummary writes a compaction summary in the background with the
// small-model role. manual is /compact or /context compact, which report
// when there is nothing to do.
func (m AppModel) startSummary(focus string, manual bool) (AppModel, tea.Cmd) {
	c, ok := m.llmClient.(ContextCompactor)
	if !ok {
		if manual {
			m.chat = m.chat.AddSystemMessage("Compaction isn't available for this client.")
		}
		return m, nil
	}
	if m.summarizing {
		if manual {
			m.chat = m.chat.AddSystemMessage("A context summary is already being written.")
		}
		return m, nil
	}
	msgs := m.chat.GetLLMMessages()
	if len(msgs) == 0 {
		if manual {
			m.chat = m.chat.AddSystemMessage("Nothing to compact yet.")
		}
		return m, nil
	}
	m.summarizing = true
	m.chat = m.chat.AddSystemMessage("🗜 Summarizing older context…")
	snapshot := append([]ChatMessage(nil), msgs...)
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), summaryTimeout)
		defer cancel()
		out, err := c.SummarizeContext(ctx, snapshot, focus)
		return ContextSummarizedMsg{
			Outcome:      out,
			Err:          err,
			snapshotLen:  len(snapshot),
			firstContent: snapshot[0].Content,
			manual:       manual,
		}
	}
}

// applySummary replaces the summarized history. Messages sent while the
// summary was being written were only appended, and pruning only swaps
// tool-result content in place, so the first Cut LLM messages are still the
// ones that were summarized.
func (m AppModel) applySummary(msg ContextSummarizedMsg) AppModel {
	m.summarizing = false
	if msg.Err != nil {
		if msg.manual || !isNothingToSummarize(msg.Err) {
			m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Context summary not applied: %v", msg.Err))
		}
		return m
	}
	current := m.chat.GetLLMMessages()
	if len(current) < msg.snapshotLen || len(current) == 0 || current[0].Content != msg.firstContent {
		m.chat = m.chat.AddSystemMessage("Context summary discarded: the conversation changed while it was being written.")
		return m
	}
	m.chat = m.chat.ApplySummary(msg.Outcome.Cut, msg.Outcome.Messages)
	if m.contextTracker != nil {
		m.contextTracker.CompactionCount++
		if msg.Outcome.TokensAfter > 0 {
			m.contextTracker.CurrentTokens = msg.Outcome.TokensAfter
			m.header = m.header.SetContextUsage(m.contextTracker.CurrentTokens, m.contextTracker.MaxTokens)
		}
	}
	m.chat = m.chat.AddSystemMessage("🗜 Context compacted: " + msg.Outcome.Line)
	LogInfo("context summarized: " + msg.Outcome.Line)
	m.persistSession()
	return m
}

// ErrNothingToSummarize is what SummarizeContext returns when there is no
// older history worth summarizing; automatic summaries stay quiet about it.
var ErrNothingToSummarize = errors.New("nothing to summarize: the older history is too small to shrink")

func isNothingToSummarize(err error) bool { return errors.Is(err, ErrNothingToSummarize) }

// ContextHandoff is implemented by clients that can write a handoff
// summary of the whole conversation (/handoff, #174).
type ContextHandoff interface {
	HandoffContext(ctx context.Context, msgs []ChatMessage, focus string) (string, error)
}

// HandoffReadyMsg delivers the text a /handoff session opens with.
type HandoffReadyMsg struct {
	Text string
	Err  error
	// snapshotLen and firstContent identify the conversation summarized.
	snapshotLen  int
	firstContent string
}

// startHandoff summarizes the whole conversation in the background. When it
// is ready the current session is saved, a new one starts, and the summary
// waits in the input for the user to edit and send.
func (m AppModel) startHandoff(focus string) (AppModel, tea.Cmd) {
	c, ok := m.llmClient.(ContextHandoff)
	if !ok {
		m.chat = m.chat.AddSystemMessage("Handoff isn't available for this client.")
		return m, nil
	}
	if m.summarizing {
		m.chat = m.chat.AddSystemMessage("A context summary is already being written.")
		return m, nil
	}
	msgs := m.chat.GetLLMMessages()
	if len(msgs) == 0 {
		m.chat = m.chat.AddSystemMessage("Nothing to hand off yet.")
		return m, nil
	}
	m.summarizing = true
	m.chat = m.chat.AddSystemMessage("🤝 Writing handoff notes…")
	snapshot := append([]ChatMessage(nil), msgs...)
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), summaryTimeout)
		defer cancel()
		text, err := c.HandoffContext(ctx, snapshot, focus)
		return HandoffReadyMsg{
			Text:         text,
			Err:          err,
			snapshotLen:  len(snapshot),
			firstContent: snapshot[0].Content,
		}
	}
}

// applyHandoff starts the new session with the handoff notes in the input.
func (m AppModel) applyHandoff(msg HandoffReadyMsg) AppModel {
	m.summarizing = false
	if msg.Err != nil {
		m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Handoff failed: %v", msg.Err))
		return m
	}
	current := m.chat.GetLLMMessages()
	if len(current) != msg.snapshotLen || current[0].Content != msg.firstContent {
		m.chat = m.chat.AddSystemMessage("Handoff discarded: the conversation changed while the notes were being written. Run /handoff again.")
		return m
	}
	m.persistSession()
	if m.sessionManager != nil {
		if s, ok := m.sessionManager.NewSession().(Session); ok {
			m.currentSession = s
		}
	}
	m.chat = m.chat.Clear()
	if m.contextTracker != nil {
		m.contextTracker.CurrentTokens = 0
		m.header = m.header.SetContextUsage(0, m.contextTracker.MaxTokens)
	}
	m.input = m.input.SetValue(msg.Text)
	m.chat = m.chat.AddSystemMessage("🤝 New session started. The handoff notes are in the input: edit them and press Enter to send.")
	return m
}
