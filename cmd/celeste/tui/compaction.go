package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// SummaryTimeouter is a client that knows how long one summary request may
// run: its request cap, the bound a chat turn gets (#345). The summary
// client fails a request that stalls for its stall timeout before that.
// ContextCompactor and ContextHandoff require it, so the chat has no
// summary deadline of its own (audit C6).
type SummaryTimeouter interface {
	SummaryTimeout() time.Duration
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
	timeout := c.SummaryTimeout()
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
			m.chat = m.chat.AddSystemMessage("Context summary not applied: " + errorText(msg.Err))
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
			m = m.syncContextBar() // K1: the bar follows the header
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
	SummaryTimeouter
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
	m.handingOff = true
	m.handoffCancelled = false
	// Input queued before /handoff waits with what is typed during it, so
	// a queued follow-up does not reach the new session ahead of the
	// notes; steers first, as dispatchQueued would send them.
	m.handoffHeld = append(append(m.handoffHeld, m.steerQueue...), m.followUpQueue...)
	m.steerQueue, m.followUpQueue = nil, nil
	m.status = m.status.SetText(handoffStatus)
	m.chat = m.chat.AddSystemMessage("🤝 Writing handoff notes… (Esc cancels)")
	snapshot := append([]ChatMessage(nil), msgs...)
	ctx, cancel := context.WithTimeout(context.Background(), c.SummaryTimeout())
	m.handoffCancel = cancel
	return m, func() tea.Msg {
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

// handoffStatus is the status bar text while /handoff writes its notes.
const handoffStatus = "🤝 Handoff in progress: input waits for the new session · Esc cancels"

// cancelHandoff stops a running handoff. Its HandoffReadyMsg still arrives
// and takes the failure path, which releases held input to the current
// session.
func (m AppModel) cancelHandoff() AppModel {
	if m.handoffCancel != nil {
		m.handoffCancel()
		m.handoffCancel = nil
	}
	m.handoffCancelled = true
	if m.status.text == handoffStatus {
		m.status = m.status.SetText("Cancelling handoff…")
	}
	return m
}

// isQuitWord reports whether lowered input is one of the words that quit.
func isQuitWord(lower string) bool {
	switch lower {
	case "exit", "quit", "q", ":q", ":quit", ":exit":
		return true
	}
	return false
}

// isLegacyTextCommand reports whether input is a command rather than chat
// text: a slash command or one of the bare legacy command words.
func isLegacyTextCommand(content string) bool {
	if strings.HasPrefix(content, "/") {
		return true
	}
	lower := strings.ToLower(content)
	return legacyCommands[lower] != nil || isQuitWord(lower)
}

// holdForHandoff keeps input submitted during a handoff away from the
// session it is replacing (#352).
func (m AppModel) holdForHandoff(content string) AppModel {
	m.handoffHeld = append(m.handoffHeld, content)
	m.chat = m.chat.AddSystemMessage("⏳ Held until the handoff's new session is ready: " + truncateQueued(content))
	return m
}

// releaseHandoffHeld hands the input held during a handoff on. In a new
// session (started) chat text joins the notes in the input, where the user
// sends it with them, and commands run there; when the handoff did not
// apply, everything runs in the session that is still current, in order.
func (m AppModel) releaseHandoffHeld(started bool) AppModel {
	held := m.handoffHeld
	m.handoffHeld = nil
	m.handingOff = false
	if !started {
		m.followUpQueue = append(m.followUpQueue, held...)
		return m
	}
	var text []string
	for _, h := range held {
		if isLegacyTextCommand(h) {
			m.followUpQueue = append(m.followUpQueue, h)
		} else {
			text = append(text, h)
		}
	}
	if len(text) > 0 {
		parts := append([]string{m.input.Value()}, text...)
		m.input = m.input.SetValue(strings.Join(parts, "\n\n"))
	}
	return m
}

// applyHandoff starts the new session with the handoff notes in the input.
func (m AppModel) applyHandoff(msg HandoffReadyMsg) AppModel {
	m.summarizing = false
	if m.handoffCancel != nil {
		m.handoffCancel()
		m.handoffCancel = nil
	}
	if m.status.text == handoffStatus || m.status.text == "Cancelling handoff…" {
		m.status = m.status.SetText("Ready")
	}
	if m.handoffCancelled {
		m.handoffCancelled = false
		m.chat = m.chat.AddSystemMessage("Handoff cancelled: this session continues.")
		return m.releaseHandoffHeld(false)
	}
	if msg.Err != nil {
		m.chat = m.chat.AddSystemMessage("Handoff failed: " + errorText(msg.Err))
		return m.releaseHandoffHeld(false)
	}
	current := m.chat.GetLLMMessages()
	if len(current) != msg.snapshotLen || current[0].Content != msg.firstContent {
		m.chat = m.chat.AddSystemMessage("Handoff discarded: the conversation changed while the notes were being written. Run /handoff again.")
		return m.releaseHandoffHeld(false)
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
	return m.releaseHandoffHeld(true)
}

// syncContextBar shows the context tracker's count in the in-chat context
// bar, the way the header shows it.
func (m AppModel) syncContextBar() AppModel {
	t := m.contextTracker
	if t == nil || t.MaxTokens <= 0 {
		return m
	}
	budgetMsg := ContextBudgetMsg{
		UsedTokens:   t.CurrentTokens,
		MaxTokens:    t.MaxTokens,
		UsagePercent: float64(t.CurrentTokens) / float64(t.MaxTokens) * 100,
	}
	if t.Budget != nil {
		budgetMsg.CompactCount = t.Budget.CompactCount
		budgetMsg.TurnCount = t.Budget.TurnCount
	}
	m.contextBar, _ = m.contextBar.Update(budgetMsg)
	return m
}

// resetContextForNewSession starts the token tracking over for the session
// /handoff, /clear or /session new just opened: a fresh tracker bound to it (the old one kept writing
// the old session's token count and turn counter), a zeroed header, and a
// context bar that keeps only the window size (V15). The ⚙ tool status row
// starts empty too (#398 C1).
func (m AppModel) resetContextForNewSession() AppModel {
	if m.contextTracker != nil {
		if s, ok := m.currentSession.(*config.Session); ok && s != nil {
			m.contextTracker = config.NewContextTracker(s, m.contextTracker.Model, m.contextTracker.MaxTokens)
		}
		m.contextTracker.CurrentTokens = 0
		m.header = m.header.SetContextUsage(0, m.contextTracker.MaxTokens)
	}
	m.contextBar = m.contextBar.resetUsage()
	m.skills = m.skills.ResetStatus()
	return m
}
