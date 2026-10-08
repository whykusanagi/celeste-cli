package tui

import (
	"strings"
	"testing"
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
