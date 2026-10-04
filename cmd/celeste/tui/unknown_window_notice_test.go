package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// #319: the unknown-window warning showed in the TUI as "⚠ / wrong.". The
// whole notice must reach the screen, wrapped, and its prefix must be a
// sign every width function agrees is one cell (no emoji presentation
// selector, which lipgloss measures as two and go-runewidth as one).
//
// The view must also fit narrow terminals: below 77 columns a fixed-width
// section used to pad every row past the edge, so each line wrapped and the
// notice broke apart.
func TestUnknownWindowNotice_RendersWhole(t *testing.T) {
	for _, width := range []int{60, 80, 100} {
		t.Run(fmt.Sprintf("width=%d", width), func(t *testing.T) {
			testUnknownWindowNoticeAt(t, width)
		})
	}
}

func testUnknownWindowNoticeAt(t *testing.T, width int) {
	// The notice is shown once per model, so each width gets its own.
	model := fmt.Sprintf("claude-notice-render-test-%d", width)
	s := &config.Session{}
	s.SetEndpoint("anthropic")
	s.SetModel(model)
	client := &endpointClient{ep: ActiveEndpoint{Provider: "anthropic", BaseURL: "https://api.anthropic.com", Model: model}}
	m := NewApp(client).WithEndpoint("anthropic")
	m = m.SetConfig(&config.Config{BaseURL: "https://api.anthropic.com", Model: model})
	m = m.SetSessionManager(&fakeSessions{session: s}, s)
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	m = next.(AppModel)

	text := chatText(m)
	if !strings.Contains(text, model) {
		t.Fatalf("no unknown-window notice in the chat:\n%s", text)
	}
	if strings.ContainsRune(text, '\uFE0F') {
		t.Errorf("notice carries an emoji presentation selector: %q", text)
	}
	view := stripANSI(m.View())
	flat := strings.Join(strings.Fields(view), " ")
	for _, want := range []string{model, "context window", "context_limit", "wrong."} {
		if !strings.Contains(flat, want) {
			t.Errorf("rendered notice lacks %q:\n%s", want, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line wider than the terminal (%d > %d): %q", w, width, line)
		}
	}
}
