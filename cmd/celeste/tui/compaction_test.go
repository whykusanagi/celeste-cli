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

var errFakeOverflow = errors.New("fake overflow")

// fakeCompactClient records compaction calls and prunes every tool result it
// is asked about when forced, or when used is over half the window.
type fakeCompactClient struct {
	fakeToolLLMClient
	calls  []bool // force flag per call
	always bool   // prune even when not forced
}

func (f *fakeCompactClient) CompactContext(msgs []ChatMessage, window, used int, force bool) (map[string]string, string, int) {
	f.calls = append(f.calls, force)
	if !force && !f.always {
		return nil, "", 0
	}
	edits := map[string]string{}
	for _, m := range msgs {
		if m.Role == "tool" && !strings.HasPrefix(m.Content, "[pruned") {
			edits[m.ToolCallID] = "[pruned " + m.ToolCallID + "]"
		}
	}
	return edits, "pruned for test", 100 * len(edits)
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

var _ tea.Model = AppModel{}
