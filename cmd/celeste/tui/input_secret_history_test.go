package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Aikido 806869355: a set-key command's key is never kept in the input
// history, which is saved with the session and its exports.
func TestInputHistoryMasksSetKeyCommands(t *testing.T) {
	for _, line := range []string{"/config set-key sk-test-123", "/voice set-key el-test-456"} {
		m := NewInputModel().Focus().SetValue(line)
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		hist := m.GetHistory()
		if len(hist) != 1 {
			t.Fatalf("history = %q", hist)
		}
		if strings.Contains(hist[0], "test-") || !strings.HasSuffix(hist[0], "set-key ***") {
			t.Errorf("%q kept as %q", line, hist[0])
		}
	}
	m := NewInputModel().Focus().SetValue("/config set-model m")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.GetHistory(); len(got) != 1 || got[0] != "/config set-model m" {
		t.Errorf("other commands are kept as typed: %q", got)
	}
}
