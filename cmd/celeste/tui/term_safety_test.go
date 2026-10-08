package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// hostile carries the controls a terminal acts on: CR, an erase-line CSI,
// an OSC title write, a C1 CSI and a bidi override.
const hostile = "echo ok\r\x1b[2K\x1b]0;t\x07\xc2\x9b2J\xe2\x80\xaesafe"

// assertInert fails when view still carries a control a terminal would act
// on. SGR color sequences celeste adds itself are allowed.
func assertInert(t *testing.T, where, view string) {
	t.Helper()
	for _, bad := range []string{"\r", "\x07", "\x1b]", "\x1b[2K", "\xc2\x9b", "\xe2\x80\xae"} {
		if strings.Contains(view, bad) {
			t.Errorf("%s: view keeps %q: %q", where, bad, view)
		}
	}
}

// Aikido 806869890: the approval prompt shows the tool and its input
// exactly, with no control left live that could rewrite the line.
func TestPermissionPromptEscapesControls(t *testing.T) {
	m := NewPermissionPromptModel()
	m.SetSize(120, 40)
	m, _ = m.Update(PermissionRequestMsg{ToolName: "bash\x1b[1A", InputSummary: hostile, RiskLevel: "destructive"})
	v := m.View()
	assertInert(t, "permission prompt", v)
	if !strings.Contains(v, "safe") || !strings.Contains(v, `\r`) {
		t.Errorf("escaped input not shown: %q", v)
	}
}

// Aikido 806869890: the ask prompt's question, labels and descriptions.
func TestAskPromptEscapesControls(t *testing.T) {
	m := NewAskPromptModel()
	m.SetSize(120, 40)
	m, _ = m.Update(AskRequestMsg{Question: "pick\n" + hostile, Options: []AskOption{
		{Label: "yes" + hostile, Description: "d" + hostile}, {Label: "no"},
	}})
	assertInert(t, "ask prompt", m.View())
}

// Aikido 806869437: workspace, tool and model text in the chat (replies
// through markdown or not, user and system lines, the Ctrl+K tool log)
// keeps no live control; celeste's own colors survive.
func TestChatRenderEscapesControls(t *testing.T) {
	osc52 := "\x1b]52;c;Zm9v\x07"
	c := NewChatModel().SetSize(120, 60)
	c = c.AddUserMessage("pasted " + hostile)
	c = c.AddAssistantMessage("# Title\n\nsome **markdown** reply " + osc52 + hostile)
	c = c.AddAssistantMessage("plain reply " + osc52)
	styled := "\x1b[38;2;255;0;128mstyled by celeste\x1b[0m"
	c = c.AddSystemMessage(styled + " path " + osc52 + hostile)
	c = c.AddPlainSystemMessage("plain " + osc52 + hostile)
	c = c.AddFunctionCall(FunctionCall{Name: "read_file" + osc52, Arguments: map[string]any{"path": "a" + osc52}, Status: "executing"})
	c = c.UpdateFunctionResult("", "read_file"+osc52, "x"+osc52+"y")
	c = c.ToggleSkillCalls()
	v := c.View()
	assertInert(t, "chat", v)
	if strings.Contains(v, `\x1b[38;2`) {
		t.Errorf("celeste's own SGR styling was escaped: %q", v)
	}
}

// The typing cursor's corruption glyphs are celeste's own styling and stay
// styled while the reply text before them is escaped.
func TestChatTypingCursorKeepsStyling(t *testing.T) {
	c := NewChatModel().SetSize(120, 40).AddAssistantMessage("")
	c = c.SetTypingActive(true)
	cursor := " \x1b[35mglyphs\x1b[0m"
	c = c.SetLastAssistantTyping("typed "+hostile, cursor)
	v := c.View()
	assertInert(t, "typing", v)
	if strings.Contains(v, `\x1b[35m`) {
		t.Errorf("typing cursor styling escaped: %q", v)
	}
}

// Every TUI frame is terminal-safe, whatever panel a string reaches: a
// directory or branch name in the status line, for one.
func TestAppViewIsTerminalSafe(t *testing.T) {
	m := NewApp(nil)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mm.(AppModel)
	m.statusLine = m.statusLine.SetProject("repo\x1b]52;c;Zm9v\x07" + hostile)
	assertInert(t, "app view", m.View())
}

// Aikido 806869738: workspace server names and transports in the /mcp
// panel are shown escaped.
func TestMCPPanelEscapesControls(t *testing.T) {
	p := NewMCPPanelModel()
	p.SetSize(120, 40)
	p.servers = []MCPServerInfo{
		{Name: "a" + hostile, Transport: "stdio" + hostile, Connected: true, ToolCount: 1},
		{Name: "b" + hostile, Enabled: true},
	}
	p.active = true
	for cur := range p.servers {
		p.cursor = cur
		assertInert(t, "mcp panel", p.View())
	}
}

// Aikido 806869747: model IDs and descriptions come from the provider's
// model list and are shown escaped in the selector.
func TestSelectorEscapesControls(t *testing.T) {
	m := NewSelectorModel("Models", []SelectorItem{
		{ID: "m1", DisplayName: "model" + hostile, Description: "desc" + hostile, Badge: "b" + hostile},
		{ID: "m2", DisplayName: "other" + hostile},
	})
	assertInert(t, "selector", m.View())
}
