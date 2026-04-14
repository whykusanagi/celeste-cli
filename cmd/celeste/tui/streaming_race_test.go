package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStreamChunkThenTick_ShortFirstChunk_DoesNotCommitEarly reproduces the
// v1.9.0 "O" truncation bug captured in
// ~/.celeste/logs/celeste_2026-04-13.log. The scenario is:
//
//  1. First streaming chunk arrives carrying 1 character (e.g. "O").
//     StreamChunkMsg with IsFirst=true starts the typing animation.
//  2. The first TickMsg fires and advances typingPos by charsPerTick=3,
//     which immediately exceeds len("O")=1. Pre-fix, the tick-complete
//     branch committed "O" to session history and stopped the ticker.
//  3. Subsequent chunks ("kay, here's the answer...") append to
//     typingContent but find the ticker dead — nothing rendered them.
//  4. The eventual StreamDoneMsg with the full 390-char FullContent
//     updated typingContent but couldn't un-commit the already-persisted
//     "O", leaving the TUI display and the session message both at 1 char.
//
// The fix: TickMsg now keeps the ticker alive when typingPos catches up
// to len(typingContent) as long as streamDone==false, treating "caught up"
// as idle rather than done. The commit branch only fires when BOTH
// streamDone AND typingPos == len(typingContent) hold.
func TestStreamChunkThenTick_ShortFirstChunk_DoesNotCommitEarly(t *testing.T) {
	model := NewApp(nil)
	model.currentSession = nil // no session persistence for this unit test

	// Step 1: first chunk arrives with a single character.
	m, _ := model.Update(StreamChunkMsg{
		Chunk: StreamChunk{Content: "O", IsFirst: true},
	})
	mm := m.(AppModel)
	require.True(t, mm.streaming, "streaming must be true after first chunk")
	require.False(t, mm.streamDone, "streamDone must be false while stream is still in flight")
	require.Equal(t, "O", mm.typingContent)

	// Step 2: a TickMsg fires — pre-fix this would commit "O" and stop.
	// With the fix, the tick-complete branch sees streamDone==false and
	// keeps the ticker alive without committing.
	m2, _ := mm.Update(TickMsg{})
	mm2 := m2.(AppModel)
	assert.True(t, mm2.streaming, "streaming must STILL be true after first tick caught up to 1-char buffer (ticker is idling, not committing)")
	assert.Equal(t, "O", mm2.typingContent, "typingContent should still be the first chunk (no second chunk yet)")

	// Step 3: second chunk arrives and extends the typing buffer.
	m3, _ := mm2.Update(StreamChunkMsg{
		Chunk: StreamChunk{Content: "kay, here's the full answer", IsFirst: false},
	})
	mm3 := m3.(AppModel)
	assert.True(t, mm3.streaming)
	assert.Equal(t, "Okay, here's the full answer", mm3.typingContent,
		"second chunk must append to typingContent without overwriting")
	assert.False(t, mm3.streamDone, "still waiting for StreamDoneMsg")

	// Step 4: StreamDoneMsg arrives with the final full content — typing
	// animation is still mid-drain. streamDone flips to true.
	m4, _ := mm3.Update(StreamDoneMsg{
		FullContent:  "Okay, here's the full answer",
		FinishReason: "stop",
	})
	mm4 := m4.(AppModel)
	assert.True(t, mm4.streamDone, "StreamDoneMsg must flip streamDone=true")
	assert.Equal(t, "Okay, here's the full answer", mm4.typingContent,
		"FullContent should be in typingContent after StreamDoneMsg")

	// Step 5: keep ticking until typing catches up. With charsPerTick=3
	// and a 28-char buffer we need at most 10 ticks to drain. The
	// tick-complete branch will fire when typingPos == len(buffer) AND
	// streamDone == true, committing the final content.
	final := mm4
	for i := 0; i < 20; i++ {
		m5, _ := final.Update(TickMsg{})
		final = m5.(AppModel)
		if !final.streaming {
			break
		}
	}
	assert.False(t, final.streaming, "typing should finish within 20 ticks")
	assert.False(t, final.streamDone, "streamDone should be reset to false after commit")
	assert.Equal(t, "", final.typingContent, "typingContent should be cleared after commit")
}

// TestStreamChunkIsFirst_ResetsStreamDone verifies that starting a new
// stream (IsFirst=true) resets streamDone to false so the tick-idle
// behavior is re-armed for the new response.
func TestStreamChunkIsFirst_ResetsStreamDone(t *testing.T) {
	model := NewApp(nil)
	model.streamDone = true // stale state from a previous stream

	m, _ := model.Update(StreamChunkMsg{
		Chunk: StreamChunk{Content: "new response", IsFirst: true},
	})
	mm := m.(AppModel)
	assert.False(t, mm.streamDone, "starting a new stream must reset streamDone")
	assert.True(t, mm.streaming)
}

// TestStreamDoneMsg_SetsStreamDone verifies the flag flip.
func TestStreamDoneMsg_SetsStreamDone(t *testing.T) {
	model := NewApp(nil)
	model.streaming = true
	model.typingContent = "partial"
	model.typingPos = 3

	m, _ := model.Update(StreamDoneMsg{
		FullContent:  "partial content finalized",
		FinishReason: "stop",
	})
	mm := m.(AppModel)
	assert.True(t, mm.streamDone, "StreamDoneMsg must set streamDone=true")
}

// TestAgentResponse_SimulatedTyping_SetsStreamDone verifies that the
// non-streaming agent response path also sets streamDone=true before
// starting the typing animation. Without that, the tick-complete branch
// would enter its new !streamDone idle loop and never commit the agent
// reply to session history.
func TestAgentResponse_SimulatedTyping_SetsStreamDone(t *testing.T) {
	model := NewApp(nil)

	m, _ := model.Update(AgentProgressMsg{
		Kind:     AgentProgressResponse,
		Text:     "the agent finished",
		Duration: 0,
	})
	mm := m.(AppModel)
	assert.True(t, mm.streamDone, "agent response path must set streamDone=true before typing")
	assert.Equal(t, "the agent finished", mm.typingContent)
}

// Silence unused-import if any assertion above is removed later.
var _ = tea.Msg(nil)
