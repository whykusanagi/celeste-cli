package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// refreshCountingClient counts RefreshSystemPrompt calls.
type refreshCountingClient struct {
	fakeAgentLLMClient
	refreshes int
}

func (c *refreshCountingClient) RefreshSystemPrompt() { c.refreshes++ }

func auditKey(m AppModel, k tea.KeyMsg) AppModel {
	updated, _ := m.Update(k)
	return updated.(AppModel)
}

// W-P1: closing /persona after a change rebuilds the system prompt, like
// /user and /confirm, so the new sliders reach the next turn.
func TestPersonaCloseRefreshesSystemPrompt(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			client := &refreshCountingClient{}
			m := newAuditApp(t, client, sz.w, sz.h)
			m = auditSend(t, m, "/persona")
			if m.viewMode != "persona" {
				t.Fatalf("viewMode = %q, want persona", m.viewMode)
			}
			m = auditKey(m, tea.KeyMsg{Type: tea.KeyRight})
			m = auditKey(m, tea.KeyMsg{Type: tea.KeyEsc})

			if client.refreshes != 1 {
				t.Errorf("RefreshSystemPrompt calls = %d, want 1", client.refreshes)
			}
			frame := auditView(m)
			if !strings.Contains(frame, "Persona sliders saved.") {
				t.Errorf("frame missing save notice:\n%s", frame)
			}
			assertFrameFits(t, frame, sz.w, sz.h)

			// Closing without a change does not rebuild the prompt.
			m = auditSend(t, m, "/persona")
			m = auditKey(m, tea.KeyMsg{Type: tea.KeyEsc})
			if client.refreshes != 1 {
				t.Errorf("unchanged close refreshed: calls = %d, want 1", client.refreshes)
			}
		})
	}
}
