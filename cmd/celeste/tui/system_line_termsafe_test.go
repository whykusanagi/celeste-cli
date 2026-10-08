package tui

import (
	"errors"
	"strings"
	"testing"
)

// hideSGR is a well-formed SGR the frame pass keeps: black on black hides
// the text after it.
const hideSGR = "\x1b[30;40m"

type hostileDiffClient struct{ fakeCheckpointClient }

func (hostileDiffClient) SessionChanges() (string, error) {
	return "Files changed this session:\n  a.txt " + hideSGR + "hidden" + hostile, nil
}

func (hostileDiffClient) UndoLastChange() (string, error) {
	return "Restored " + hideSGR + "a.txt" + hostile, nil
}

type hostileAgentsClient struct{ fakeToolLLMClient }

func (hostileAgentsClient) ListSubagents() []SubagentInfo {
	return []SubagentInfo{{
		ID: "id" + hideSGR, TaskID: "t" + hideSGR, Name: "n" + hideSGR + hostile,
		Status: "running", Type: "explore" + hideSGR, Summary: "s" + hideSGR + hostile,
	}}
}

func (hostileAgentsClient) KillSubagent(string) bool { return true }

func assertNoESCInSystemLines(t *testing.T, where string, m AppModel) {
	t.Helper()
	for _, msg := range m.chat.GetMessages() {
		if msg.Role == "system" && strings.ContainsAny(msg.Content, "\x1b\r\x07") {
			t.Errorf("%s: system line keeps a control: %q", where, msg.Content)
		}
	}
}

// Aikido 806869437 follow-up: workspace and model text interpolated into
// /diff, /undo and /agents system lines is escaped before it is styled,
// so not even a kept SGR color can hide it.
func TestDiffAndAgentsSystemLinesEscapeWorkspaceText(t *testing.T) {
	m := sendCommand(NewApp(&hostileDiffClient{}), "/diff")
	assertNoESCInSystemLines(t, "/diff", m)
	if !hasSystemMessageContaining(m.chat.GetMessages(), "Files changed this session:\n  a.txt") {
		t.Errorf("/diff lost its text")
	}
	m = sendCommand(m, "/undo")
	assertNoESCInSystemLines(t, "/undo", m)

	m = sendCommand(NewApp(&hostileAgentsClient{}), "/agents")
	assertNoESCInSystemLines(t, "/agents", m)
	if !hasSystemMessageContaining(m.chat.GetMessages(), "Subagents:") {
		t.Errorf("/agents listing missing")
	}
	m = sendCommand(m, "/agents kill x"+hideSGR)
	assertNoESCInSystemLines(t, "/agents kill", m)
}

// Provider and tool errors shown in system and status lines, and an
// /agent run's output and error, are escaped before they are styled.
func TestErrorAndAgentResultLinesEscapeControls(t *testing.T) {
	if got := errorText(errors.New("error, boom " + hideSGR + hostile)); strings.ContainsAny(got, "\x1b\r\x07") {
		t.Errorf("errorText keeps a control: %q", got)
	}
	m := NewApp(&fakeToolLLMClient{})
	mm, _ := m.Update(AgentProgressMsg{Kind: AgentProgressError, Text: "bad " + hideSGR + hostile})
	assertNoESCInSystemLines(t, "agent progress error", mm.(AppModel))
	mm, _ = NewApp(&fakeToolLLMClient{}).Update(AgentCommandResultMsg{Output: "out " + hideSGR + hostile})
	assertNoESCInSystemLines(t, "agent command output", mm.(AppModel))
}
