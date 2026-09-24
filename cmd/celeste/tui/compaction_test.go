package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

var errFakeOverflow = errors.New("fake overflow")

// fakeCompactClient records compaction calls. It prunes every tool result
// when forced (or always, if set), and summarizes the first half of the
// history into one message.
type fakeCompactClient struct {
	fakeToolLLMClient
	calls     []bool // force flag per prune call
	always    bool   // prune even when not forced
	stillOver bool   // report the history still over the threshold
	summaries []string
	sumErr    error
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

func (f *fakeCompactClient) SummarizeContext(_ context.Context, msgs []ChatMessage, focus string) (SummaryOutcome, error) {
	f.summaries = append(f.summaries, focus)
	if f.sumErr != nil {
		return SummaryOutcome{}, f.sumErr
	}
	cut := len(msgs) / 2
	return SummaryOutcome{
		Cut:      cut,
		Messages: []ChatMessage{{Role: "user", Content: "<compacted-context>summary</compacted-context>"}},
		Line:     "summarized for test",
	}, nil
}

func (f *fakeCompactClient) IsContextOverflow(err error) bool { return errors.Is(err, errFakeOverflow) }

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
	m, _ = step(t, m, toolBatch("call_a"))
	m, _ = step(t, m, SkillResultMsg{Name: "tool_a", Result: "big result", ToolCallID: "call_a"})
	return m
}

// /context compact forces a prune and replaces the tool results (#174).
func TestContextCompactCommand(t *testing.T) {
	m, client := newCompactTestApp(t)
	m = runToolTurn(t, m)
	m, _ = step(t, m, StreamDoneMsg{}) // end the turn

	m, _ = step(t, m, SendMessageMsg{Content: "/context compact"})
	require.NotEmpty(t, client.calls)
	assert.Empty(t, client.summaries, "pruning was enough; no summary needed")
	assert.True(t, client.calls[len(client.calls)-1], "/context compact must force")
	assert.Equal(t, "[pruned call_a]", toolContent(m, "call_a"))
	assert.Equal(t, 1, m.contextTracker.CompactionCount)
}

// Compaction is checked at every tool boundary, before the follow-up request.
func TestCompactionCheckedAtToolBoundary(t *testing.T) {
	m, client := newCompactTestApp(t)
	client.always = true
	m = runToolTurn(t, m)
	require.Len(t, client.sendCalls, 2, "user send + follow-up")
	assert.Equal(t, "[pruned call_a]", toolContent(m, "call_a"),
		"the follow-up request should go out with the pruned history")
}

// A context-overflow error prunes harder and resends once; a second overflow
// is reported.
func TestOverflowCompactsAndResendsOnce(t *testing.T) {
	m, client := newCompactTestApp(t)
	m = runToolTurn(t, m)
	sends := len(client.sendCalls)

	m, _ = step(t, m, StreamErrorMsg{Err: errFakeOverflow})
	assert.Len(t, client.sendCalls, sends+1, "overflow should resend once after compacting")
	assert.True(t, m.streaming)

	m, _ = step(t, m, StreamErrorMsg{Err: errFakeOverflow})
	assert.Len(t, client.sendCalls, sends+1, "a second overflow must not resend again")
	assert.False(t, m.streaming)
}

// runCmd executes a command and feeds every message it produces back in.
func runCmd(t *testing.T, m AppModel, cmd tea.Cmd) AppModel {
	t.Helper()
	for _, msg := range collectMsgs(cmd) {
		if msg == nil {
			continue
		}
		if _, ok := msg.(ContextSummarizedMsg); ok {
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
	m, _ = step(t, m, StreamDoneMsg{})
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
	m, _ = step(t, m, StreamDoneMsg{})
	m, cmd := step(t, m, SendMessageMsg{Content: "/compact"})

	m.chat = m.chat.Clear() // the user cleared the chat meanwhile
	m.chat = m.chat.AddUserMessage("new topic")
	m = runCmd(t, m, cmd)

	msgs := m.chat.GetLLMMessages()
	require.Len(t, msgs, 1)
	assert.Equal(t, "new topic", msgs[0].Content)
}

// When pruning leaves the history over the threshold, a summary starts
// automatically at the tool boundary.
func TestAutoSummaryWhenPruningIsNotEnough(t *testing.T) {
	m, client := newCompactTestApp(t)
	client.stillOver = true
	m = runToolTurn(t, m)
	assert.True(t, m.summarizing, "an automatic summary should be in flight")
	assert.Empty(t, client.summaries, "it runs in the background, not inline")
}

var _ tea.Model = AppModel{}
