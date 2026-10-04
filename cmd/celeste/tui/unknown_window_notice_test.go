package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// #319: the unknown-window warning showed in the TUI as "⚠ / wrong.". The
// whole notice must reach the screen, wrapped, and its prefix must be a
// sign every width function agrees is one cell (no emoji presentation
// selector, which lipgloss measures as two and go-runewidth as one).
func TestUnknownWindowNotice_RendersWhole(t *testing.T) {
	s := &config.Session{}
	s.SetEndpoint("anthropic")
	s.SetModel("claude-notice-render-test")
	client := &endpointClient{ep: ActiveEndpoint{Provider: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-notice-render-test"}}
	m := NewApp(client).WithEndpoint("anthropic")
	m = m.SetConfig(&config.Config{BaseURL: "https://api.anthropic.com", Model: "claude-notice-render-test"})
	m = m.SetSessionManager(&fakeSessions{session: s}, s)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(AppModel)

	text := chatText(m)
	if !strings.Contains(text, "claude-notice-render-test") {
		t.Fatalf("no unknown-window notice in the chat:\n%s", text)
	}
	if strings.ContainsRune(text, '\uFE0F') {
		t.Errorf("notice carries an emoji presentation selector: %q", text)
	}
	view := ansi.Strip(m.View())
	flat := strings.Join(strings.Fields(view), " ")
	for _, want := range []string{"claude-notice-render-test", "context window", "context_limit", "wrong."} {
		if !strings.Contains(flat, want) {
			t.Errorf("rendered notice lacks %q:\n%s", want, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(line); w > 100 {
			t.Errorf("line wider than the terminal (%d > 100): %q", w, line)
		}
	}
}
