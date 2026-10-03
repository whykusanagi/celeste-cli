package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// tallPlanQuestion is a submit_plan approval question with n steps, each
// with a detail line (the shape the builtin tool sends).
func tallPlanQuestion(n int) string {
	var b strings.Builder
	b.WriteString("Approve this plan?\n\nGoal: tidy the parser\n\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%d. Step number %d\n   detail for step %d\n", i, i, i)
	}
	return strings.TrimRight(b.String(), "\n")
}

// visibleRows is what the terminal shows: bubbletea drops the view's top
// lines when it is taller than the terminal.
func visibleRows(view string, h int) string {
	lines := strings.Split(view, "\n")
	if len(lines) > h {
		lines = lines[len(lines)-h:]
	}
	return strings.Join(lines, "\n")
}

func tallAskApp(t *testing.T, h int) tea.Model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var m tea.Model = NewApp(nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: h})
	m, _ = m.Update(AskRequestMsg{
		Question: tallPlanQuestion(12),
		Options: []AskOption{
			{Label: "Approve and start", Description: "Save the plan."},
			{Label: "Keep planning", Description: "Stay in plan mode."},
		},
		Response: make(chan AskResponseMsg, 1),
	})
	return m
}

// A long plan's approval modal must fit the terminal: its start (the
// question and step 1) and its options stay on screen, and the rest is
// reached by scrolling, so nobody approves steps they could not see.
func TestAskPromptTallPlanFitsTerminal(t *testing.T) {
	const h = 30
	m := tallAskApp(t, h)
	view := m.View()
	if got := strings.Count(view, "\n") + 1; got > h {
		t.Fatalf("the view is %d rows on a %d-row terminal:\n%s", got, h, view)
	}
	shown := visibleRows(view, h)
	for _, want := range []string{"Approve this plan?", "1. Step number 1", "Approve and start", "Keep planning", "PgDn"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("the screen lacks %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "12. Step number 12") {
		t.Fatalf("step 12 should need scrolling at %d rows:\n%s", h, shown)
	}

	// Scrolling down reaches the last step; the options stay visible.
	for range 10 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	shown = visibleRows(m.View(), h)
	for _, want := range []string{"12. Step number 12", "detail for step 12", "Approve and start", "Keep planning"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("after PgDn the screen lacks %q:\n%s", want, shown)
		}
	}

	// And back up to the start.
	for range 10 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	}
	if shown = visibleRows(m.View(), h); !strings.Contains(shown, "Approve this plan?") {
		t.Fatalf("after PgUp the screen lacks the question:\n%s", shown)
	}
}

// A question that fits is shown whole, with no scroll hint.
func TestAskPromptShortQuestionHasNoScrollHint(t *testing.T) {
	m := NewAskPromptModel()
	m.SetSize(100, 20)
	m, _ = m.Update(AskRequestMsg{Question: "color?", Options: []AskOption{{Label: "red"}}, Response: make(chan AskResponseMsg, 1)})
	v := m.View()
	if !strings.Contains(v, "color?") || strings.Contains(v, "PgDn") {
		t.Fatalf("short question view:\n%s", v)
	}
}

// The split-panel path sizes the modal too.
func TestAskPromptTallPlanFitsSplitPanel(t *testing.T) {
	const h = 30
	m := tallAskApp(t, h)
	m, _ = m.Update(OrchestratorEventMsg{Kind: 0, Lane: "code", Text: "90% confidence"})
	if !m.(AppModel).splitPanelMode {
		t.Fatal("an orchestrator event did not open the split panel")
	}
	shown := visibleRows(m.View(), h)
	for _, want := range []string{"Approve this plan?", "1. Step number 1", "Approve and start", "Keep planning"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("the split-panel screen lacks %q:\n%s", want, shown)
		}
	}
}
