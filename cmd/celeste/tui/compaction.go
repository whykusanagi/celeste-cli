package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// defaultSummaryTimeout bounds a summary when the client reports no cap of
// its own: llm.MaxRequestDuration of the default 60 s stall timeout.
const defaultSummaryTimeout = 30 * time.Minute

// SummaryTimeouter is a client that knows how long one summary request may
// run: its request cap, the bound a chat turn gets (#345). The summary
// client fails a request that stalls for its stall timeout before that.
type SummaryTimeouter interface {
	SummaryTimeout() time.Duration
}

// summaryTimeout bounds a compaction summary or a handoff: the client's
// request cap, else defaultSummaryTimeout. A fixed 3 minutes cut off a cold
// local model's summary while its chat turns succeeded (#345).
func (m AppModel) summaryTimeout() time.Duration {
	if c, ok := m.llmClient.(SummaryTimeouter); ok {
		if d := c.SummaryTimeout(); d > 0 {
			return d
		}
	}
	return defaultSummaryTimeout
}

// ContextSummarizedMsg delivers a compaction summary written in the
// background (#174).
type ContextSummarizedMsg struct {
	Outcome SummaryOutcome
	Err     error
	// snapshot is the history the summary was written from. A summary is
	// applied only while the chat's first Outcome.Cut LLM messages are
	// still those (summaryStillFits), so a stale one (after /clear, a
	// session switch, or anything that rewrote the head) is dropped.
	snapshot []ChatMessage
	manual   bool
}

// compactContext prunes old tool results when the history is over the
// compaction threshold, or unconditionally when force is set (/context
// compact). During a turn the loop's compactor prunes instead.
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

// startSummaryAs writes a compaction summary in the background with the
// small-model role. trigger ("manual" or "auto") is passed to
// PreCompact/PostCompact hooks.
func (m AppModel) startSummaryAs(focus string, manual bool, trigger string) (AppModel, tea.Cmd) {
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
	timeout := m.summaryTimeout()
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), compactTriggerKey{}, trigger), timeout)
		defer cancel()
		out, err := c.SummarizeContext(ctx, snapshot, focus)
		return ContextSummarizedMsg{
			Outcome:  out,
			Err:      err,
			snapshot: snapshot,
			manual:   manual,
		}
	}
}

// startSummary is startSummaryAs with the trigger derived from manual.
func (m AppModel) startSummary(focus string, manual bool) (AppModel, tea.Cmd) {
	trigger := "auto"
	if manual {
		trigger = "manual"
	}
	return m.startSummaryAs(focus, manual, trigger)
}

type compactTriggerKey struct{}

// CompactionTrigger reports what started a summary, "manual" or "auto",
// for PreCompact/PostCompact hooks.
func CompactionTrigger(ctx context.Context) string {
	if t, _ := ctx.Value(compactTriggerKey{}).(string); t != "" {
		return t
	}
	return "auto"
}

// applySummary replaces the summarized history when it still fits and
// reports whether it did. Messages sent while the summary was being written
// were only appended, and pruning only swaps tool-result content in place,
// so the first Cut LLM messages are normally still the ones summarized. A
// discarded summary leaves summarizing off, so the next automatic summary
// can try again.
func (m AppModel) applySummary(msg ContextSummarizedMsg) (AppModel, bool) {
	m.summarizing = false
	if msg.Err != nil {
		switch {
		case msg.manual || !isNothingToSummarize(msg.Err):
			m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Context summary not applied: %v", msg.Err))
		default:
			// "Summarizing older context…" is on screen; close it (#234).
			m.chat = m.chat.AddSystemMessage("🗜 Summary skipped: the older history is too small to shrink.")
		}
		return m, false
	}
	if !summaryStillFits(m.chat.GetLLMMessages(), msg.snapshot, msg.Outcome.Cut) {
		m.chat = m.chat.AddSystemMessage("Context summary discarded: the conversation changed while it was being written.")
		return m, false
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
	return m, true
}

// summaryStillFits reports whether current still starts with the first cut
// messages of snapshot. Tool results are compared by call ID only (pruning
// rewrites their content); every other message by role, content and tool
// call IDs.
func summaryStillFits(current, snapshot []ChatMessage, cut int) bool {
	if len(snapshot) == 0 || cut < 0 || cut > len(snapshot) || len(current) < len(snapshot) {
		return false
	}
	for i := 0; i < cut; i++ {
		a, b := current[i], snapshot[i]
		if a.Role != b.Role || a.ToolCallID != b.ToolCallID || len(a.ToolCalls) != len(b.ToolCalls) {
			return false
		}
		for j := range a.ToolCalls {
			if a.ToolCalls[j].ID != b.ToolCalls[j].ID {
				return false
			}
		}
		if a.Role != "tool" && a.Content != b.Content {
			return false
		}
	}
	return true
}

// ErrNothingToSummarize is what SummarizeContext returns when there is no
// older history worth summarizing; an automatic summary reports it as skipped.
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
	timeout := m.summaryTimeout()
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	m = m.resetContextForNewSession()
	m.input = m.input.SetValue(msg.Text)
	m.chat = m.chat.AddSystemMessage("🤝 New session started. The handoff notes are in the input: edit them and press Enter to send.")
	return m
}

// resetContextForNewSession starts the token tracking over for the session
// /handoff, /clear or /session new just opened: a fresh tracker bound to it (the old one kept writing
// the old session's token count and turn counter), a zeroed header, and a
// context bar that keeps only the window size (V15).
func (m AppModel) resetContextForNewSession() AppModel {
	if m.contextTracker != nil {
		if s, ok := m.currentSession.(*config.Session); ok && s != nil {
			m.contextTracker = config.NewContextTracker(s, m.contextTracker.Model, m.contextTracker.MaxTokens)
		}
		m.contextTracker.CurrentTokens = 0
		m.header = m.header.SetContextUsage(0, m.contextTracker.MaxTokens)
	}
	m.contextBar = m.contextBar.resetUsage()
	return m
}
