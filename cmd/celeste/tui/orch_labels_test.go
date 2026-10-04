package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// V21: the split panel must not print the lane "unknown" with a made-up
// confidence, and a model-tagged action shows its model once.
func TestOrchFeedLabels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var m tea.Model = NewApp(nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m, _ = m.Update(OrchestratorEventMsg{Kind: 0, Lane: "unknown", Text: "10% confidence"})
	m, _ = m.Update(OrchestratorEventMsg{Kind: 1, Lane: "unknown", Model: "fugu", Text: "primary agent"})
	acts := m.(AppModel).splitPanel.Actions()
	if len(acts) != 2 {
		t.Fatalf("actions = %q", acts)
	}
	if strings.Contains(acts[0], "unknown") || strings.Contains(acts[0], "confidence") {
		t.Errorf("unmatched lane shown as %q", acts[0])
	}
	if !strings.Contains(acts[0], "no lane matched") {
		t.Errorf("classified line = %q, want it to say no lane matched", acts[0])
	}
	if strings.Count(acts[1], "[fugu]") != 1 {
		t.Errorf("model label not shown exactly once: %q", acts[1])
	}
}
