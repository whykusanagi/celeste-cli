package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// fakeCompactClient records compaction calls. It prunes every tool result
// when forced (or always, if set), and summarizes the first half of the
// history into one message.
type fakeCompactClient struct {
	fakeToolLLMClient
	calls     []bool // force flag per prune call
	always    bool   // prune even when not forced
	stillOver bool   // report the history still over the threshold
	summaries []string
	handoffs  []string
	// handoffBlock makes HandoffContext wait until its context ends.
	handoffBlock bool
	sumErr       error
	// summaryCap is what SummaryTimeout reports; deadlines records how far
	// away each summary's and handoff's deadline was (#345).
	summaryCap time.Duration
	deadlines  []time.Duration
	// tokensAfter is the estimate a summary reports (K1).
	tokensAfter int
	// contextTokens is the next request's size a summary reports (#400).
	contextTokens int
}

// SummaryTimeout is summaryCap, or the default 30-minute request cap.
func (f *fakeCompactClient) SummaryTimeout() time.Duration {
	if f.summaryCap > 0 {
		return f.summaryCap
	}
	return 30 * time.Minute
}

func (f *fakeCompactClient) recordDeadline(ctx context.Context) {
	if dl, ok := ctx.Deadline(); ok {
		f.deadlines = append(f.deadlines, time.Until(dl))
	} else {
		f.deadlines = append(f.deadlines, 0)
	}
}

func (f *fakeCompactClient) CompactContext(msgs []ChatMessage, window, used int, force bool) CompactOutcome {
	f.calls = append(f.calls, force)
	out := CompactOutcome{StillOver: f.stillOver}
	if !force && !f.always {
		return out
	}
	out.Edits = map[string]string{}
	for _, m := range msgs {
		if m.Role == "tool" && !strings.HasPrefix(m.Content, "[pruned") {
			out.Edits[m.ToolCallID] = "[pruned " + m.ToolCallID + "]"
		}
	}
	if len(out.Edits) == 0 {
		out.Edits = nil
	}
	out.Summary = "pruned for test"
	out.SavedTokens = 100 * len(out.Edits)
	return out
}

func (f *fakeCompactClient) SummarizeContext(ctx context.Context, msgs []ChatMessage, focus string) (SummaryOutcome, error) {
	f.recordDeadline(ctx)
	f.summaries = append(f.summaries, focus)
	if f.sumErr != nil {
		return SummaryOutcome{}, f.sumErr
	}
	cut := len(msgs) / 2
	return SummaryOutcome{
		Cut:           cut,
		Messages:      []ChatMessage{{Role: "user", Content: "<compacted-context>summary</compacted-context>"}},
		Line:          "summarized for test",
		TokensAfter:   f.tokensAfter,
		ContextTokens: f.contextTokens,
	}, nil
}

func (f *fakeCompactClient) HandoffContext(ctx context.Context, msgs []ChatMessage, focus string) (string, error) {
	f.recordDeadline(ctx)
	f.handoffs = append(f.handoffs, focus)
	if f.handoffBlock {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if f.sumErr != nil {
		return "", f.sumErr
	}
	return "handoff notes for " + msgs[0].Content, nil
}

func newCompactTestApp(t *testing.T) (AppModel, *fakeCompactClient) {
	t.Helper()
	client := &fakeCompactClient{fakeToolLLMClient: fakeToolLLMClient{skills: []SkillDefinition{{Name: "tool_a"}}}}
	m := NewApp(client)
	m.skillsEnabled = true
	m.contextTracker = config.NewContextTracker(&config.Session{}, "test-model", 100_000)
	m.contextTracker.CurrentTokens = 50_000
	return m, client
}

func toolContent(m AppModel, id string) string {
	for _, msg := range m.chat.GetLLMMessages() {
		if msg.Role == "tool" && msg.ToolCallID == id {
			return msg.Content
		}
	}
	return ""
}

func runToolTurn(t *testing.T, m AppModel) AppModel {
	t.Helper()
	m, _ = step(t, m, SendMessageMsg{Content: "go"})
	return toolTurn(t, m, "call_a")
}

// /context compact forces a prune and replaces the tool results (#174).
func TestContextCompactCommand(t *testing.T) {
	m, client := newCompactTestApp(t)
	m = runToolTurn(t, m)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})

	m, _ = step(t, m, SendMessageMsg{Content: "/context compact"})
	require.NotEmpty(t, client.calls)
	assert.Empty(t, client.summaries, "pruning was enough; no summary needed")
	assert.True(t, client.calls[len(client.calls)-1], "/context compact must force")
	assert.Equal(t, "[pruned call_a]", toolContent(m, "call_a"))
	assert.Equal(t, 1, m.contextTracker.CompactionCount)
}

// runCmd executes a command and feeds every message it produces back in.
func runCmd(t *testing.T, m AppModel, cmd tea.Cmd) AppModel {
	t.Helper()
	for _, msg := range collectMsgs(cmd) {
		if msg == nil {
			continue
		}
		switch msg.(type) {
		case ContextSummarizedMsg, HandoffReadyMsg:
			m, _ = step(t, m, msg)
		}
	}
	return m
}

// /compact [focus] writes a summary in the background and swaps it in: the
// summarized messages stay in the scrollback but are no longer sent, and the
// summary is sent in their place (#174).
func TestCompactCommandAppliesSummary(t *testing.T) {
	m, client := newCompactTestApp(t)
	m = runToolTurn(t, m)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	before := m.chat.GetLLMMessages()
	shown := len(m.chat.GetMessages())

	m, cmd := step(t, m, SendMessageMsg{Content: "/compact the parser work"})
	require.True(t, m.summarizing)
	m = runCmd(t, m, cmd)

	require.Equal(t, []string{"the parser work"}, client.summaries)
	assert.False(t, m.summarizing)
	after := m.chat.GetLLMMessages()
	require.NotEmpty(t, after)
	assert.True(t, strings.HasPrefix(after[0].Content, "<compacted-context>"), "the summary should lead the LLM history")
	assert.Equal(t, len(before)-len(before)/2+1, len(after), "cut messages replaced by one summary")
	assert.GreaterOrEqual(t, len(m.chat.GetMessages()), shown, "scrollback must keep the summarized messages")
}

// A summary written for a conversation that has since changed is dropped.
func TestStaleSummaryIsDiscarded(t *testing.T) {
	m, _ := newCompactTestApp(t)
	m = runToolTurn(t, m)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	m, cmd := step(t, m, SendMessageMsg{Content: "/compact"})

	m.chat = m.chat.Clear() // the user cleared the chat meanwhile
	m.chat = m.chat.AddUserMessage("new topic")
	m = runCmd(t, m, cmd)

	msgs := m.chat.GetLLMMessages()
	require.Len(t, msgs, 1)
	assert.Equal(t, "new topic", msgs[0].Content)
}

// /handoff summarizes the whole conversation, starts a fresh chat and leaves
// the notes in the input for the user to edit and send (#174).
func TestHandoffStartsFreshChatWithNotes(t *testing.T) {
	m, client := newCompactTestApp(t)
	m = runToolTurn(t, m)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})

	m, cmd := step(t, m, SendMessageMsg{Content: "/handoff the parser"})
	require.True(t, m.summarizing)
	m = runCmd(t, m, cmd)

	require.Equal(t, []string{"the parser"}, client.handoffs)
	assert.False(t, m.summarizing)
	assert.Empty(t, m.chat.GetLLMMessages(), "the new chat starts empty")
	assert.Equal(t, "handoff notes for go", m.input.Value())
	assert.Equal(t, 0, m.contextTracker.CurrentTokens)
}

// A failed handoff leaves the conversation alone.
func TestHandoffFailureKeepsConversation(t *testing.T) {
	m, client := newCompactTestApp(t)
	client.sumErr = errors.New("boom")
	m = runToolTurn(t, m)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	before := len(m.chat.GetLLMMessages())

	m, cmd := step(t, m, SendMessageMsg{Content: "/handoff"})
	m = runCmd(t, m, cmd)

	assert.Len(t, m.chat.GetLLMMessages(), before)
	assert.Empty(t, m.input.Value())
}

var _ tea.Model = AppModel{}

func TestSummaryStillFits(t *testing.T) {
	snapshot := []ChatMessage{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "tool_a"}}},
		{Role: "tool", ToolCallID: "c1", Content: "full result"},
		{Role: "user", Content: "d"},
	}
	with := func(i int, f func(*ChatMessage)) []ChatMessage {
		out := make([]ChatMessage, len(snapshot))
		copy(out, snapshot)
		out[1].ToolCalls = append([]ToolCallInfo(nil), snapshot[1].ToolCalls...)
		f(&out[i])
		return out
	}
	cases := []struct {
		name    string
		current []ChatMessage
		snap    []ChatMessage
		cut     int
		want    bool
	}{
		{"identical", snapshot, snapshot, 3, true},
		{"longer current", append(append([]ChatMessage(nil), snapshot...), ChatMessage{Role: "assistant", Content: "e"}), snapshot, 3, true},
		{"tool result pruned mid-turn still fits", with(2, func(m *ChatMessage) { m.Content = "[pruned]" }), snapshot, 3, true},
		{"change after the cut", with(3, func(m *ChatMessage) { m.Content = "other" }), snapshot, 3, true},
		{"role mismatch", with(0, func(m *ChatMessage) { m.Role = "assistant" }), snapshot, 3, false},
		{"tool call ID mismatch", with(2, func(m *ChatMessage) { m.ToolCallID = "c2" }), snapshot, 3, false},
		{"tool calls ID mismatch", with(1, func(m *ChatMessage) { m.ToolCalls[0].ID = "c9" }), snapshot, 3, false},
		{"tool calls count mismatch", with(1, func(m *ChatMessage) { m.ToolCalls = nil }), snapshot, 3, false},
		{"content mismatch", with(0, func(m *ChatMessage) { m.Content = "z" }), snapshot, 3, false},
		{"current shorter than snapshot", snapshot[:3], snapshot, 3, false},
		{"cut beyond snapshot", append(append([]ChatMessage(nil), snapshot...), snapshot...), snapshot, 5, false},
		{"negative cut", snapshot, snapshot, -1, false},
		{"empty snapshot", snapshot, nil, 0, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, summaryStillFits(tc.current, tc.snap, tc.cut), tc.name)
	}
}

// The turn carries the window the loop's compactor prunes against.
func TestTurnRequestCarriesTheWindow(t *testing.T) {
	m, client := newCompactTestApp(t)
	step(t, m, SendMessageMsg{Content: "go"})
	require.Len(t, client.turns, 1)
	assert.Equal(t, 100_000, client.turns[0].req.Window)
}

// #234 caveat 3: "🗜 Summarizing older context…" never got an outcome line
// when an automatic summary found nothing to summarize.
func TestAutomaticSummarySkipSaysSo(t *testing.T) {
	m, _ := newCompactTestApp(t)
	m.summarizing = true
	m, applied := m.applySummary(ContextSummarizedMsg{Err: ErrNothingToSummarize, manual: false})
	if applied {
		t.Fatal("nothing should be applied")
	}
	shown := m.chat.GetMessages()
	require.NotEmpty(t, shown, "no outcome line")
	last := shown[len(shown)-1]
	if !strings.Contains(last.Content, "Summary skipped") {
		t.Fatalf("last line = %q, want the skip notice", last.Content)
	}
}

// #345: a summary and a handoff are bounded by the client's request cap,
// the one a chat turn gets, not a fixed 3 minutes.
func TestSummaryDeadlineIsTheClientCap(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cap      time.Duration
		min, max time.Duration
	}{
		{"client cap", 60 * time.Minute, 59 * time.Minute, 60 * time.Minute},
		{"short cap", 5 * time.Minute, 4 * time.Minute, 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, client := newCompactTestApp(t)
			client.summaryCap = tc.cap
			m = runToolTurn(t, m)
			m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
			m, cmd := step(t, m, SendMessageMsg{Content: "/compact"})
			m = runCmd(t, m, cmd)
			m, cmd = step(t, m, SendMessageMsg{Content: "/handoff"})
			_ = runCmd(t, m, cmd)
			require.Len(t, client.deadlines, 2, "one summary and one handoff")
			for _, left := range client.deadlines {
				assert.GreaterOrEqual(t, left, tc.min)
				assert.LessOrEqual(t, left, tc.max)
			}
		})
	}
}

// After /compact the in-chat context bar shows the post-compaction count, as
// the header does, instead of the last turn's (K1).
func TestCompactUpdatesContextBar(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m, client := newCompactTestApp(t)
			client.tokensAfter = 13_000
			sized, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			m = sized.(AppModel)
			m = runToolTurn(t, m)
			m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
			m, _ = step(t, m, ContextBudgetMsg{UsedTokens: 50_000, MaxTokens: 100_000, UsagePercent: 50, TurnCount: 4})
			require.Contains(t, auditView(m), "50.0K / 100.0K")

			m, cmd := step(t, m, SendMessageMsg{Content: "/compact"})
			m = runCmd(t, m, cmd)

			assert.Equal(t, 13_000, m.contextTracker.CurrentTokens)
			assert.Equal(t, 13_000, m.contextBar.usedTokens, "the bar must follow the compaction")
			frame := auditView(m)
			assert.Contains(t, frame, "◆ tokens: 13.0K / 100.0K", frame)
			assert.NotContains(t, frame, "50.0K / 100.0K")
			assertFrameFits(t, frame, sz.w, sz.h)
		})
	}
}

// Review: a prune during a turn (CompactedMsg) moves the bar with the
// header too.
func TestTurnCompactionUpdatesContextBar(t *testing.T) {
	m, _ := newCompactTestApp(t)
	m, _ = step(t, m, SendMessageMsg{Content: "go"})
	m, _ = step(t, m, ContextBudgetMsg{UsedTokens: 50_000, MaxTokens: 100_000, UsagePercent: 50})
	m, _ = feed(t, m, CompactedMsg{Line: "pruned", Saved: 20_000})
	assert.Equal(t, 30_000, m.contextTracker.CurrentTokens)
	assert.Equal(t, 30_000, m.contextBar.usedTokens)
}

// #400: after an automatic summary the header and the context bar show what
// the next request will send (system prompt, tool schemas, the summary and
// the kept messages), not the summary's own size. The smoke showed
// "469 / 32.8K (1%)" while the next request was 16.6K (51%).
func TestAutoSummaryShowsTheNextRequestSize(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m, client := newCompactTestApp(t)
			client.tokensAfter = 469
			client.contextTokens = 16_600
			m.contextTracker.MaxTokens = 32_800
			sized, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			m = sized.(AppModel)
			m = runToolTurn(t, m)
			m, _ = step(t, m, ContextBudgetMsg{UsedTokens: 30_000, MaxTokens: 32_800, UsagePercent: 91, TurnCount: 4})

			m, cmd := feed(t, m, TurnDoneMsg{Stop: "done", Summarize: true})
			require.True(t, m.summarizing, "the turn asked for a summary")
			m = runCmd(t, m, cmd)
			require.False(t, m.summarizing)

			assert.Equal(t, 16_600, m.contextTracker.CurrentTokens)
			assert.Equal(t, 16_600, m.contextBar.usedTokens, "the bar must show the next request")
			frame := auditView(m)
			assert.Contains(t, frame, "16.6K / 32.8K", frame)
			assert.NotContains(t, frame, "469 / 32.8K", frame)
			assertFrameFits(t, frame, sz.w, sz.h)
		})
	}
}

// Messages sent while the summary was being written are counted on top of
// the size the summary reported.
func TestSummaryCountsMessagesSentWhileItWasWritten(t *testing.T) {
	m, client := newCompactTestApp(t)
	client.contextTokens = 10_000
	m = runToolTurn(t, m)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	m, cmd := step(t, m, SendMessageMsg{Content: "/compact"})
	msgs := collectMsgs(cmd)
	m.chat = m.chat.AppendLLM(ChatMessage{Role: "user", Content: strings.Repeat("x", 4000)})
	for _, msg := range msgs {
		if s, ok := msg.(ContextSummarizedMsg); ok {
			m, _ = step(t, m, s)
		}
	}
	assert.Equal(t, 10_000+EstimateMessageTokens(ChatMessage{Role: "user", Content: strings.Repeat("x", 4000)}), m.contextTracker.CurrentTokens)
}

// Review: a message sent during the summary counts like the compactor
// counts it, tool-call arguments and framing included, so the bar does not
// read low until the next reply.
func TestSummaryCountsToolCallArgumentsSentWhileItWasWritten(t *testing.T) {
	sent := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c9", Name: "write_file", Arguments: strings.Repeat("a", 8000)}}},
		{Role: "user", Content: strings.Repeat("x", 400)},
	}
	got := summaryContextTokens(SummaryOutcome{ContextTokens: 10_000}, sent)
	want := 10_000 + EstimateMessageTokens(sent[0]) + EstimateMessageTokens(sent[1])
	assert.Equal(t, want, got)
	assert.Greater(t, EstimateMessageTokens(sent[0]), 2_000, "tool-call arguments must count")
}
