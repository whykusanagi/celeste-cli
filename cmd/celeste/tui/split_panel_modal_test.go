package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// C1: /orch lanes ask the permission modal (F2c) and may call the ask tool.
// Update routes keys to both in split-panel mode, so View must render them
// there too, or the run waits on a prompt nobody can see.
func TestSplitPanelViewShowsPermissionAndAskPrompts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, tc := range []struct {
		name string
		msg  tea.Msg
		want []string
	}{
		{"permission", PermissionRequestMsg{ToolName: "write_file", InputSummary: "out.txt", RiskLevel: "write", Response: make(chan PermissionResponse, 1)}, []string{"Permission Required", "write_file"}},
		{"ask", AskRequestMsg{Question: "Which file?", Options: []AskOption{{Label: "a.go"}}, Response: make(chan AskResponseMsg, 1)}, []string{"Which file?", "a.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var m tea.Model = NewApp(nil)
			m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
			m, _ = m.Update(OrchestratorEventMsg{Kind: 0, Lane: "code", Text: "90% confidence"})
			if !m.(AppModel).splitPanelMode {
				t.Fatal("an orchestrator event did not open the split panel")
			}
			m, _ = m.Update(tc.msg)
			view := m.View()
			for _, w := range tc.want {
				if !strings.Contains(view, w) {
					t.Fatalf("the split-panel view lacks %q:\n%s", w, view)
				}
			}
		})
	}
}
