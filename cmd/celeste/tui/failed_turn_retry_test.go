package tui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func userCount(msgs []ChatMessage) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "user" {
			n++
		}
	}
	return n
}

// L5: a turn that failed before any reply leaves its prompt unanswered.
// Sending the same prompt again retries it: the request carries it once,
// not `user, user`.
func TestRetryAfterAFailedTurnSendsThePromptOnce(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "hello"})
	m, _ = feed(t, m, TurnDoneMsg{Stop: "error", Err: errors.New("no data from the model for 10m0s")})
	assert.True(t, hasLine(m, "send it again"), "the failure line should say how to retry")

	m, _ = step(t, m, SendMessageMsg{Content: "hello"})
	require.Len(t, client.turns, 2)
	assert.Equal(t, 1, userCount(client.turns[1].req.History), "the retry duplicated the prompt")
	assert.Equal(t, 1, userCount(m.chat.GetLLMMessages()))
}

// A different message after a failed turn is new input: the unanswered
// prompt stays as context and the new one follows it.
func TestNewPromptAfterAFailedTurnKeepsTheUnansweredOne(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "hello"})
	m, _ = feed(t, m, TurnDoneMsg{Stop: "error", Err: errors.New("boom")})
	_, _ = step(t, m, SendMessageMsg{Content: "are you there?"})
	require.Len(t, client.turns, 2)
	assert.Equal(t, 2, userCount(client.turns[1].req.History))
}

// An answered prompt sent again is a new question, never merged.
func TestAnsweredPromptSentAgainIsAppended(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "hello"})
	history := append(append([]ChatMessage(nil), m.chat.GetLLMMessages()...), ChatMessage{Role: "assistant", Content: "hi"})
	m, _ = feed(t, m, HistoryMsg{History: history})
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	m, _ = step(t, m, SendMessageMsg{Content: "hello"})
	require.Len(t, client.turns, 2)
	assert.Equal(t, 2, userCount(client.turns[1].req.History))
}

// A turn that failed after its tool calls ran has an answer in progress;
// the prompt is not marked for reuse.
func TestFailedTurnAfterToolCallsIsNotReused(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "hello"})
	m = toolTurn(t, m, "call_a")
	m, _ = feed(t, m, TurnDoneMsg{Stop: "error", Err: errors.New("boom")})
	_, _ = step(t, m, SendMessageMsg{Content: "hello"})
	require.Len(t, client.turns, 2)
	assert.Equal(t, 2, userCount(client.turns[1].req.History))
}
