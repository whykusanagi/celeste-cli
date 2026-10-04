package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// auditSizes are the two terminal sizes the pre-2.0 TUI audit rendered at.
var auditSizes = []struct {
	name string
	w, h int
}{
	{"80x24", 80, 24},
	{"120x40", 120, 40},
}

// newAuditApp builds a sized AppModel with a hermetic HOME, the way the audit
// rendered it: sakana endpoint, model fugu, no live client.
func newAuditApp(t *testing.T, client LLMClient, w, h int) AppModel {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := NewApp(client).SetVersion("2.0.0", "test").WithEndpoint("sakana").SetWorkDir(t.TempDir())
	app.model = "fugu"
	sized, _ := app.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return sized.(AppModel)
}

// auditSend submits one line of input as if the user pressed Enter.
func auditSend(t *testing.T, m AppModel, content string) AppModel {
	t.Helper()
	updated, _ := m.Update(SendMessageMsg{Content: content})
	return updated.(AppModel)
}

// auditView returns the composed frame with ANSI stripped.
func auditView(m AppModel) string { return stripANSI(m.View()) }

// lastSystemRender renders the newest system message exactly as the chat
// viewport does (markdown path included), ANSI stripped.
func lastSystemRender(t *testing.T, m AppModel, width int) string {
	t.Helper()
	msgs := m.chat.GetMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "system" {
			return stripANSI(m.chat.renderMessageOpt(msgs[i], width, false))
		}
	}
	t.Fatal("no system message")
	return ""
}

// assertFrameFits checks the frame is exactly h rows and no row exceeds w cells.
func assertFrameFits(t *testing.T, frame string, w, h int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) > h {
		t.Errorf("frame has %d rows, want <= %d", len(lines), h)
	}
	for i, l := range lines {
		if n := visibleWidth(l); n > w {
			t.Errorf("row %d is %d cells wide, want <= %d: %q", i, n, w, l)
		}
	}
}

func visibleWidth(s string) int {
	return lipgloss.Width(s)
}
