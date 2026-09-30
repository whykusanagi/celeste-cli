package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// fakeTurn is a TurnHandle the test drives: feed delivers its events.
type fakeTurn struct {
	req       TurnRequest
	steers    []string
	late      []string // returned once by Leftover
	cancelled bool
}

func (t *fakeTurn) Steer(s string) { t.steers = append(t.steers, s) }
func (t *fakeTurn) Cancel()        { t.cancelled = true }

func (t *fakeTurn) Leftover() []string {
	s := t.late
	t.late = nil
	return s
}

// startedTurn adds prompt to the chat and starts a turn on it directly.
func startedTurn(t *testing.T, m AppModel, prompt string) AppModel {
	t.Helper()
	m.chat = m.chat.AddUserMessage(prompt)
	m, _ = m.startTurn()
	require.NotNil(t, m.turn, "startTurn did not start a turn")
	return m
}

// feed delivers msg as the running turn's next event.
func feed(t *testing.T, m AppModel, msg tea.Msg) (AppModel, tea.Cmd) {
	t.Helper()
	return step(t, m, TurnEventMsg{Run: m.turnRun, Msg: msg})
}

// toolTurn feeds one model turn that calls tool_a once per id, as the loop
// reports it: the recorded calls, each call, the results.
func toolTurn(t *testing.T, m AppModel, ids ...string) AppModel {
	t.Helper()
	history := append([]ChatMessage(nil), m.chat.GetLLMMessages()...)
	var calls []ToolCallInfo
	for _, id := range ids {
		calls = append(calls, ToolCallInfo{ID: id, Name: "tool_a", Arguments: `{}`})
	}
	history = append(history, ChatMessage{Role: "assistant", Content: "working", ToolCalls: calls})
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	m, _ = feed(t, m, ToolTurnMsg{Text: "working"})
	m, _ = feed(t, m, HistoryMsg{History: history})
	for _, id := range ids {
		m, _ = feed(t, m, ToolStartMsg{ID: id, Name: "tool_a"})
	}
	for _, id := range ids {
		m, _ = feed(t, m, ToolResultMsg{ID: id, Name: "tool_a", Content: `{"ok":true}`})
		history = append(history, ChatMessage{Role: "tool", ToolCallID: id, Name: "tool_a", Content: `{"ok":true}`})
	}
	m, _ = feed(t, m, HistoryMsg{History: history})
	return m
}

func hasLine(m AppModel, sub string) bool {
	for _, msg := range m.chat.GetMessages() {
		if msg.Role == "system" && strings.Contains(msg.Content, sub) {
			return true
		}
	}
	return false
}

func TestToolTurnRendersAndSyncsHistory(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m = toolTurn(t, m, "call_a", "call_b")
	msgs := m.chat.GetLLMMessages()
	require.Len(t, msgs, 4)
	assert.Equal(t, []string{"user", "assistant", "tool", "tool"}, []string{msgs[0].Role, msgs[1].Role, msgs[2].Role, msgs[3].Role})
	assert.Len(t, msgs[1].ToolCalls, 2, "text-free or not, the tool_calls message is recorded (#203)")
	assert.False(t, m.toolProgress.Executing(), "every call reported a result")
	assert.True(t, m.turnActive(), "the turn is running until TurnDoneMsg")
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	assert.False(t, m.turnActive())
}

func TestGuardNoticeIsShown(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = feed(t, m, TurnDoneMsg{Stop: "identical", Notice: "⚠️ Stopped: identical calls"})
	assert.Equal(t, "⚠️ Stopped: identical calls", m.chat.GetMessages()[len(m.chat.GetMessages())-1].Content)
	assert.False(t, m.turnActive())
}

// Events of a turn that already ended are dropped, and their chain stops.
func TestEventsOfAnEndedTurnAreIgnored(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	old := m.turnRun
	m, _ = feed(t, m, TurnDoneMsg{Stop: "interrupted"})
	before := len(m.chat.GetMessages())
	m, cmd := step(t, m, TurnEventMsg{Run: old, Msg: HookWarningMsg{Text: "late"}, Next: func() tea.Msg { return nil }})
	assert.Nil(t, cmd)
	assert.Len(t, m.chat.GetMessages(), before)
}

func TestCompactedEventIsShown(t *testing.T) {
	m, _ := newCompactTestApp(t)
	m = startedTurn(t, m, "go")
	m, _ = feed(t, m, CompactedMsg{Line: "pruned 1 old tool results"})
	assert.Equal(t, 1, m.contextTracker.CompactionCount)
	msgs := m.chat.GetMessages()
	assert.Equal(t, "🗜 Context compacted: pruned 1 old tool results", msgs[len(msgs)-1].Content)
}

// When the loop's pruning is not enough, a summary starts when the turn
// ends (intentional change 7: before, at a tool boundary mid-turn).
func TestTurnDoneStartsTheAutomaticSummary(t *testing.T) {
	m, client := newCompactTestApp(t)
	m = startedTurn(t, m, "go")
	m = toolTurn(t, m, "call_a")
	assert.False(t, m.summarizing, "no summary while the turn runs")
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done", Summarize: true})
	assert.True(t, m.summarizing, "an automatic summary should be in flight")
	assert.Empty(t, client.summaries, "it runs in the background, not inline")
}

// Review Focus 3: a summary that finishes mid-turn is held, so the loop's
// snapshots still line up with the chat, and is applied when the turn ends.
func TestSummaryArrivingMidTurnWaitsForTheTurnToEnd(t *testing.T) {
	m, _ := newCompactTestApp(t)
	m.chat = m.chat.AddUserMessage("old").AddAssistantMessage("old reply")
	snapshot := append([]ChatMessage(nil), m.chat.GetLLMMessages()...)
	m.summarizing = true // started before this turn
	m = startedTurn(t, m, "go")
	m, _ = step(t, m, ContextSummarizedMsg{
		Outcome:  SummaryOutcome{Cut: 2, Messages: []ChatMessage{{Role: "user", Content: "<summary>"}}, Line: "2 messages summarized"},
		snapshot: snapshot,
	})
	assert.True(t, m.summarizing, "the summary is held, not applied, while the turn runs")
	history := append(append([]ChatMessage(nil), m.chat.GetLLMMessages()...), ChatMessage{Role: "assistant", Content: "answer"})
	m, _ = feed(t, m, HistoryMsg{History: history})
	assert.Equal(t, []string{"user:old", "assistant:old reply", "user:go", "assistant:answer"}, llmRoles(m.chat),
		"the snapshot must land on an untouched history")
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	assert.False(t, m.summarizing)
	assert.Equal(t, []string{"user:<summary>", "user:go", "assistant:answer"}, llmRoles(m.chat))
}

// A held summary written from a history the chat no longer starts with is
// discarded, and the turn's own automatic summary retries.
func TestHeldSummaryThatNoLongerFitsIsDiscardedAndRetried(t *testing.T) {
	m, _ := newCompactTestApp(t)
	m.chat = m.chat.AddUserMessage("old").AddAssistantMessage("old reply")
	m.summarizing = true
	m = startedTurn(t, m, "go")
	m, _ = step(t, m, ContextSummarizedMsg{
		Outcome:  SummaryOutcome{Cut: 2, Messages: []ChatMessage{{Role: "user", Content: "<summary>"}}},
		snapshot: []ChatMessage{{Role: "user", Content: "another conversation"}, {Role: "assistant", Content: "x"}},
	})
	m, cmd := feed(t, m, TurnDoneMsg{Stop: "done", Summarize: true})
	assert.Equal(t, []string{"user:old", "assistant:old reply", "user:go"}, llmRoles(m.chat))
	assert.True(t, hasLine(m, "Context summary discarded"))
	assert.True(t, m.summarizing, "the automatic summary retries against the history as it is now")
	assert.NotNil(t, cmd)
}

// Review Focus 2: steers handed back in TurnDoneMsg and steers typed after
// the run took those (TurnHandle.Leftover) are both sent, in order.
func TestLateSteerIsSentAfterTheTurn(t *testing.T) {
	m, client := newQueueTestApp()
	m = startedTurn(t, m, "first")
	client.turns[0].late = []string{"typed after the run ended"}
	_, cmd := feed(t, m, TurnDoneMsg{Stop: "done", Leftover: []string{"never joined"}})
	next, sent := queuedSend(cmd)
	require.True(t, sent, "the leftover steers were not sent")
	assert.Equal(t, "never joined\n\ntyped after the run ended", next.Content)
	assert.Empty(t, client.turns[0].late, "Leftover was not taken")
}

// Review Focus 6: the checked-prompts snapshot marks the prompt before any
// reply, so an interrupted first request leaves it checked.
func TestPromptsCheckedSnapshotMarksThePrompt(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	marked := ChatMessage{Role: "user", Content: "go", Metadata: map[string]any{MetaPromptHookDone: true}}
	m, _ = feed(t, m, HistoryMsg{History: []ChatMessage{marked}})
	m, _ = feed(t, m, TurnDoneMsg{Stop: "interrupted"})
	msgs := m.chat.GetLLMMessages()
	require.Len(t, msgs, 1)
	assert.Equal(t, true, msgs[0].Metadata[MetaPromptHookDone])
}

// Ctrl+C cancels the turn; the turn ending as interrupted keeps the pending
// interrupt, so a second Ctrl+C within 3s quits.
func TestDoubleCtrlCQuitsAfterTheTurnEnds(t *testing.T) {
	m, client := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.True(t, client.turns[0].cancelled, "Ctrl+C did not cancel the turn")
	m, _ = feed(t, m, TurnDoneMsg{Stop: "interrupted"})
	_, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd)
	_, quit := cmd().(tea.QuitMsg)
	assert.True(t, quit, "the second Ctrl+C did not quit")
}

// An empty reply is a system line, never an empty assistant message (the
// adapter also drops the loop's empty reply from every snapshot).
func TestEmptyReplyShowsASystemLine(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	m, _ = feed(t, m, StreamDoneMsg{FinishReason: "stop"})
	m, _ = feed(t, m, HistoryMsg{History: m.chat.GetLLMMessages()})
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	msgs := m.chat.GetLLMMessages()
	assert.Equal(t, "user", msgs[len(msgs)-1].Role, "an empty assistant message joined the history")
	assert.True(t, hasLine(m, "No response"))
}

// recordingSessions keeps a copy of the messages of every save.
type recordingSessions struct{ saves [][]config.SessionMessage }

func (r *recordingSessions) NewSession() interface{} { return &config.Session{} }
func (r *recordingSessions) Save(s interface{}) error {
	if cs, ok := s.(*config.Session); ok {
		r.saves = append(r.saves, append([]config.SessionMessage(nil), cs.Messages...))
	}
	return nil
}
func (r *recordingSessions) Load(string) (interface{}, error)           { return nil, nil }
func (r *recordingSessions) List() ([]interface{}, error)               { return nil, nil }
func (r *recordingSessions) Delete(string) error                        { return nil }
func (r *recordingSessions) MergeSessions(a, _ interface{}) interface{} { return a }

// While a reply is still being typed, the chat shows the typed prefix plus
// glitch glyphs. A snapshot during typing, and a quit before the typing
// finished, must save the loop's reply, never that display text.
func TestHistoryDuringTypingNeverSavesTheTypedPrefix(t *testing.T) {
	const reply = "Hello there, this reply is long enough to still be typing"
	m, _ := newQueueTestApp()
	sessions := &recordingSessions{}
	m = m.SetSessionManager(sessions, &config.Session{})
	m = startedTurn(t, m, "go")
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	m, _ = feed(t, m, StreamChunkMsg{Chunk: StreamChunk{Content: reply[:10], IsFirst: true}})
	m, _ = step(t, m, TickMsg{})
	m, _ = feed(t, m, StreamChunkMsg{Chunk: StreamChunk{Content: reply[10:]}})
	m, _ = feed(t, m, StreamDoneMsg{FullContent: reply, FinishReason: "stop"})
	m, _ = step(t, m, TickMsg{})
	live := m.chat.GetLLMMessages()[1].Content
	require.NotEqual(t, reply, live, "the reply should still be typing")
	require.NotEmpty(t, m.typingContent)

	saved := len(sessions.saves)
	m, _ = feed(t, m, HistoryMsg{History: []ChatMessage{
		m.chat.GetLLMMessages()[0],
		{Role: "assistant", Content: reply},
	}})
	assert.Equal(t, live, m.chat.GetLLMMessages()[1].Content, "the live bubble keeps its display")
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	require.NotEmpty(t, m.typingContent, "the reply should still be typing after the turn")
	assert.Len(t, sessions.saves, saved, "nothing is saved while the reply is typing; the typing commit saves")

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	_, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd)
	_, quit := cmd().(tea.QuitMsg)
	require.True(t, quit, "the second Ctrl+C did not quit")

	require.NotEmpty(t, sessions.saves, "quitting saves the session")
	for i, save := range sessions.saves {
		for _, msg := range save {
			if msg.Role == "assistant" {
				assert.Equal(t, reply, msg.Content, "save %d holds display text", i)
			}
		}
	}
	last := sessions.saves[len(sessions.saves)-1]
	require.Len(t, last, 2)
	assert.Equal(t, reply, last[1].Content)
}

// A Ctrl+C that races a turn finishing on its own (the loop ends "done"
// before the cancel reaches it) still quits on the second press, and the
// status keeps saying so.
func TestCtrlCRacingAFinishedTurnStillQuitsOnTheSecondPress(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	assert.Contains(t, m.status.text, "Press Ctrl+C again to exit")
	_, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd)
	_, quit := cmd().(tea.QuitMsg)
	assert.True(t, quit, "the second Ctrl+C did not quit")
}

// After a Ctrl+C the "interrupted" end of the turn keeps the Ctrl+C hint
// instead of overwriting it with "Interrupted".
func TestCtrlCInterruptKeepsTheQuitHint(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	m, _ = feed(t, m, TurnDoneMsg{Stop: "interrupted"})
	assert.Equal(t, "Cancelled. Press Ctrl+C again to exit", m.status.text)
}

// Esc (not Ctrl+C) still ends with "Interrupted".
func TestEscInterruptShowsInterrupted(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = feed(t, m, TurnDoneMsg{Stop: "interrupted"})
	assert.Equal(t, "Interrupted", m.status.text)
}

// Parallel calls to the same tool: each result lands on its own call in the
// skill log (paired by call ID; by name, the first result would take the
// latest executing call and the two would swap).
func TestParallelSameToolResultsPairByCallID(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = feed(t, m, ToolStartMsg{ID: "call_a", Name: "tool_a"})
	m, _ = feed(t, m, ToolStartMsg{ID: "call_b", Name: "tool_a"})
	m, _ = feed(t, m, ToolResultMsg{ID: "call_a", Name: "tool_a", Content: "result a"})
	m, _ = feed(t, m, ToolResultMsg{ID: "call_b", Name: "tool_a", Content: "result b"})
	calls := m.chat.functionCalls
	require.Len(t, calls, 2)
	assert.Equal(t, "call_a", calls[0].ID)
	assert.Equal(t, "result a", calls[0].Result)
	assert.Equal(t, "call_b", calls[1].ID)
	assert.Equal(t, "result b", calls[1].Result)
}

// savedMessages substitutes the typed text into the last assistant reply
// only; an earlier reply and the chat itself are untouched.
func TestSavedMessagesSubstitutesOnlyTheLastReply(t *testing.T) {
	m, _ := newQueueTestApp()
	m.chat = m.chat.AddUserMessage("u1").AddAssistantMessage("first reply").
		AddUserMessage("u2").AddAssistantMessage("second re▓▒")
	m.typingContent = "second reply, whole"
	var got []string
	for _, msg := range m.savedMessages() {
		if msg.Role == "assistant" {
			got = append(got, msg.Content)
		}
	}
	assert.Equal(t, []string{"first reply", "second reply, whole"}, got)
	msgs := m.chat.GetMessages()
	assert.Equal(t, "second re▓▒", msgs[len(msgs)-1].Content, "savedMessages changed the chat")
}

// Esc while the UserPromptSubmit hook runs: the hook is cut short
// (PromptBlockedMsg.Cancelled), which is not a block. The prompt stays.
func TestEscDuringThePromptHookKeepsThePrompt(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	prompt := m.chat.GetLLMMessages()[0]
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = feed(t, m, PromptBlockedMsg{Cancelled: true, Content: prompt.Content, Timestamp: prompt.Timestamp})
	m, _ = feed(t, m, TurnDoneMsg{Stop: "interrupted"})
	assert.Equal(t, []string{"user:go"}, llmRoles(m.chat))
	assert.False(t, hasLine(m, "Prompt blocked"))
}

// A steer typed after Esc goes to the app's queue and is sent after the
// turn, never to the cancelled loop.
func TestSteerAfterEscIsQueuedForTheNextTurn(t *testing.T) {
	m, client := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = step(t, m, SendMessageMsg{Content: "after esc"})
	assert.Empty(t, client.turns[0].steers, "the steer went to the cancelled loop")
	assert.Equal(t, []string{"after esc"}, m.steerQueue)
	_, cmd := feed(t, m, TurnDoneMsg{Stop: "interrupted"})
	next, sent := queuedSend(cmd)
	require.True(t, sent)
	assert.Equal(t, "after esc", next.Content)
}

// A turn that ends in an error or a guard stop while its reply is still
// typing lets the typing commit, so the turn stops being active.
func TestTurnEndingWhileTypingCommitsTheTyping(t *testing.T) {
	for _, done := range []TurnDoneMsg{
		{Stop: "error", Err: errors.New("boom")},
		{Stop: "cap", Notice: "⚠️ Stopped: turn cap"},
	} {
		t.Run(done.Stop, func(t *testing.T) {
			m, _ := newQueueTestApp()
			m = startedTurn(t, m, "go")
			m, _ = feed(t, m, TurnStartMsg{Turn: 1})
			m, _ = feed(t, m, StreamChunkMsg{Chunk: StreamChunk{Content: "a partial reply", IsFirst: true}})
			m, _ = feed(t, m, done)
			require.NotEmpty(t, m.typingContent)
			assert.True(t, m.streamDone, "no more text is coming")
			for i := 0; i < 100 && m.typingContent != ""; i++ {
				m, _ = step(t, m, TickMsg{})
			}
			assert.Empty(t, m.typingContent, "the typing never committed")
			assert.False(t, m.turnActive())
		})
	}
}

// A held summary is cleared once handled, applied or discarded, so a later
// turn end never applies it again.
func TestHeldSummaryIsClearedAfterTheTurn(t *testing.T) {
	for _, fits := range []bool{true, false} {
		m, _ := newCompactTestApp(t)
		m.chat = m.chat.AddUserMessage("old").AddAssistantMessage("old reply")
		snapshot := append([]ChatMessage(nil), m.chat.GetLLMMessages()...)
		if !fits {
			snapshot = []ChatMessage{{Role: "user", Content: "another"}, {Role: "assistant", Content: "x"}}
		}
		m.summarizing = true
		m = startedTurn(t, m, "go")
		m, _ = step(t, m, ContextSummarizedMsg{
			Outcome:  SummaryOutcome{Cut: 2, Messages: []ChatMessage{{Role: "user", Content: "<summary>"}}, Line: "summarized"},
			snapshot: snapshot,
		})
		require.NotNil(t, m.heldSummary)
		m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
		assert.Nil(t, m.heldSummary, "fits=%v: the held summary was kept", fits)
	}
}

// A stale empty reply (a bubble an interrupt left empty) plus a keepLive
// snapshot never hides the loop's reply (Task 9 review ruling): the prompt
// is appended, or the stale reply dropped, before the turn starts.
func TestStaleEmptyReplyNeverHidesTheLoopsReply(t *testing.T) {
	stale := func() AppModel {
		m, _ := newQueueTestApp()
		m.chat = m.chat.AddUserMessage("go").AddAssistantMessage("")
		return m
	}
	t.Run("prompt", func(t *testing.T) {
		m := stale()
		m, _ = step(t, m, SendMessageMsg{Content: "again"})
		require.NotNil(t, m.turn)
		var users []ChatMessage
		for _, msg := range m.chat.GetLLMMessages() {
			if msg.Role == "user" {
				users = append(users, msg)
			}
		}
		require.Len(t, users, 2)
		m, _ = feed(t, m, TurnStartMsg{Turn: 1})
		m, _ = feed(t, m, StreamChunkMsg{Chunk: StreamChunk{Content: "Hel", IsFirst: true}})
		m, _ = feed(t, m, HistoryMsg{History: append(users, ChatMessage{Role: "assistant", Content: "Hello"})})
		m, _ = feed(t, m, StreamDoneMsg{FullContent: "Hello", FinishReason: "stop"})
		m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
		for i := 0; i < 100 && m.typingContent != ""; i++ {
			m, _ = step(t, m, TickMsg{})
		}
		assert.Equal(t, []string{"user:go", "user:again", "assistant:Hello"}, llmRoles(m.chat))
	})
	t.Run("no prompt", func(t *testing.T) {
		// A turn start that appends nothing (a retry), with a reply live.
		m := stale()
		m, _ = m.startTurn()
		require.NotNil(t, m.turn)
		m.typingContent = "Hel"
		m, _ = feed(t, m, HistoryMsg{History: []ChatMessage{m.chat.GetLLMMessages()[0], {Role: "assistant", Content: "Hello"}}})
		msgs := m.chat.GetLLMMessages()
		assert.Equal(t, "Hello", msgs[len(msgs)-1].Content, "the stale empty reply hid the loop's reply")
	})
}

// endpointRecorder records endpoint switches and model changes.
type endpointRecorder struct {
	fakeCompactClient
	switches []string
}

func (e *endpointRecorder) SwitchEndpoint(endpoint string) error {
	e.switches = append(e.switches, endpoint)
	return nil
}
func (e *endpointRecorder) ChangeModel(string) error { return nil }

// Endpoint and profile switches and /context compact wait while a turn is
// active, like other commands (Task 9 review ruling): the loop's run must
// not see the adapter's config or pruned store change under it.
func TestSwitchesAndContextCompactWaitForTheTurn(t *testing.T) {
	m, _ := newCompactTestApp(t)
	client := &endpointRecorder{}
	client.skills = []SkillDefinition{{Name: "tool_a"}}
	m.llmClient = client
	m, _ = step(t, m, SendMessageMsg{Content: "go"})
	require.NotNil(t, m.turn)
	for _, c := range []string{"/endpoint openai", "/config work", "/context compact"} {
		m, _ = step(t, m, SendMessageMsg{Content: c})
	}
	m, _ = step(t, m, SendMessageMsg{Content: "steer this #venice"})
	assert.Empty(t, client.switches, "an endpoint switch ran during the turn")
	assert.Empty(t, client.calls, "/context compact pruned during the turn")
	assert.Equal(t, []string{"/endpoint openai", "/config work", "/context compact"}, m.followUpQueue)

	m, cmd := feed(t, m, TurnDoneMsg{Stop: "done", Leftover: []string{"steer this #venice"}})
	for i := 0; i < 5; i++ {
		next, sent := queuedSend(cmd)
		if !sent {
			break
		}
		m, cmd = step(t, m, next)
		if m.turn != nil {
			m, cmd = feed(t, m, TurnDoneMsg{Stop: "done"})
		}
	}
	assert.Equal(t, []string{"venice", "openai", "work"}, client.switches, "the queued switches ran after the turn, in order")
	assert.NotEmpty(t, client.calls, "/context compact ran after the turn")
}
