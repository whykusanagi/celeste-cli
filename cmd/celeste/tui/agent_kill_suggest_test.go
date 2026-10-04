package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// W-A1: /agent has no kill subcommand, so typeahead must not offer one;
// accepting it ran an agent whose goal was "kill <id>". /agents kill stays.
func TestAgentTypeaheadDoesNotOfferKill(t *testing.T) {
	if got := computeSuggestions("/agent "); slices.Contains(got, "agent kill") {
		t.Errorf("computeSuggestions(%q) = %v, must not offer agent kill", "/agent ", got)
	}
	if got := computeSuggestions("/agents "); !slices.Contains(got, "agents kill") {
		t.Errorf("computeSuggestions(%q) = %v, want agents kill", "/agents ", got)
	}
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeAgentLLMClient{}, sz.w, sz.h)
			for _, r := range "/agent " {
				updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				m = updated.(AppModel)
			}
			frame := auditView(m)
			if !strings.Contains(frame, "list-runs") {
				t.Fatalf("typeahead row missing:\n%s", frame)
			}
			if strings.Contains(frame, "kill") {
				t.Errorf("frame offers kill for /agent:\n%s", frame)
			}
			assertFrameFits(t, frame, sz.w, sz.h)
		})
	}
}
