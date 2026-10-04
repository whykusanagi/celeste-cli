package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// The audit's two terminal sizes (docs/superpowers/notes/2026-10-03-tui-audit.md).
var auditSizes = []struct{ w, h int }{{80, 24}, {120, 40}}

// assertFitsWidth fails when any row of view is wider than w: the terminal
// would wrap it (or cut it) and the layout would lose a row.
func assertFitsWidth(t *testing.T, view string, w int) {
	t.Helper()
	for i, ln := range strings.Split(view, "\n") {
		if got := lipgloss.Width(ln); got > w {
			t.Fatalf("row %d is %d cells on a %d-column terminal: %q\n%s", i, got, w, stripANSI(ln), stripANSI(view))
		}
	}
}

func mcpPanelAt(w int) MCPPanelModel {
	p := NewMCPPanelModel()
	p.SetSize(w, 14)
	p.servers = []MCPServerInfo{
		{Name: "repo-stub", Transport: "stdio", Enabled: false},
		{Name: "stub", Transport: "stdio", Connected: true, ToolCount: 1, Enabled: true},
	}
	p.active = true
	return p
}

// V8: every box row has its right border, the box is one width, and the
// footer fits at 80 columns.
func TestMCPPanelViewBoxedAndFits(t *testing.T) {
	for _, sz := range auditSizes {
		view := mcpPanelAt(sz.w).View()
		assertFitsWidth(t, view, sz.w)
		lines := strings.Split(stripANSI(view), "\n")
		footer := lines[len(lines)-1]
		box := lines[:len(lines)-1]
		boxW := lipgloss.Width(box[0])
		for i, ln := range box {
			if lipgloss.Width(ln) != boxW {
				t.Fatalf("%dx%d: box row %d is %d cells, the top is %d:\n%s", sz.w, sz.h, i, lipgloss.Width(ln), boxW, strings.Join(lines, "\n"))
			}
			last := []rune(ln)[len([]rune(ln))-1]
			if !strings.ContainsRune("│╮╯", last) {
				t.Fatalf("%dx%d: box row %d has no right border: %q", sz.w, sz.h, i, ln)
			}
		}
		for _, want := range []string{"repo-stub", "disabled", "1 tools", "Total: 1 external tools available"} {
			if !strings.Contains(stripANSI(view), want) {
				t.Fatalf("%dx%d: the panel lacks %q:\n%s", sz.w, sz.h, want, stripANSI(view))
			}
		}
		if !strings.Contains(footer, "close") && !strings.Contains(footer, "Close") {
			t.Fatalf("%dx%d: the footer lost its close hint: %q", sz.w, sz.h, footer)
		}
	}
}

// V8: the app hint row under /mcp names Space (toggle) and not Enter,
// which does nothing in the panel.
func TestMCPHintsNameSpaceNotEnter(t *testing.T) {
	h := hintsFor("chat", true)
	if strings.Contains(h, "↵") {
		t.Fatalf("the /mcp hints list Enter, a no-op: %q", h)
	}
	if !strings.Contains(h, "space") {
		t.Fatalf("the /mcp hints omit Space: %q", h)
	}
	if lipgloss.Width(" "+h) > 80 {
		t.Fatalf("the /mcp hints wrap at 80 columns: %q", h)
	}
}

// savePickerSessions writes sessions the way the chat does: a renamed fork
// with no messages yet (like the one /fork makes) and two with a prompt,
// whose IDs share their first 8 digits.
func savePickerSessions(t *testing.T, workDir string) []string {
	t.Helper()
	mgr := config.NewSessionManager()
	var ids []string
	for _, s := range []struct{ name, prompt string }{
		{"fork of 1791090012349041000", ""},
		{"", "Reply with exactly one word: ready"},
		{"", "Continuing work from a previous session.\n\n## Goal\nCreate notes"},
	} {
		sess := mgr.NewSession()
		sess.Name = s.name
		sess.Workspace = workDir
		if s.prompt != "" {
			sess.Messages = []config.SessionMessage{{Role: "user", Content: s.prompt}}
		}
		if err := mgr.Save(sess); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sess.ID)
	}
	return ids
}

func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// V9: the picker lists what /session list lists, sessions with no messages
// included, so it never says "No saved sessions." while /session list shows
// some. V14: each row shows the whole ID (the one /session resume takes)
// and the session's name.
func TestSessionPickerListsEverySessionWithWholeIDs(t *testing.T) {
	isolatedHome(t)
	workDir := t.TempDir()
	ids := savePickerSessions(t, workDir)
	for _, sz := range auditSizes {
		p := NewSessionPanelModel(workDir).SetWidth(sz.w).SetHeight(sz.h - 9)
		view := p.View()
		plain := stripANSI(view)
		assertFitsWidth(t, view, sz.w)
		if strings.Contains(plain, "No saved sessions") || !strings.Contains(plain, "Sessions (3)") {
			t.Fatalf("%dx%d: the picker does not list all 3 sessions:\n%s", sz.w, sz.h, plain)
		}
		for _, id := range ids {
			if !strings.Contains(plain, id) {
				t.Fatalf("%dx%d: the picker lacks the whole ID %s:\n%s", sz.w, sz.h, id, plain)
			}
		}
		for _, want := range []string{"fork of 1791090012349041000", "Reply with exactly one word: ready", "this project"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("%dx%d: the picker lacks %q:\n%s", sz.w, sz.h, want, plain)
			}
		}
	}
}

// V9: the picker fills the chat area, empty or not, so the input, status
// line and hints stay at the bottom of the screen.
func TestSessionPickerFillsTheChatArea(t *testing.T) {
	for _, withSessions := range []bool{false, true} {
		isolatedHome(t)
		workDir := t.TempDir()
		if withSessions {
			savePickerSessions(t, workDir)
		}
		for _, sz := range auditSizes {
			var m tea.Model = NewApp(nil)
			m, _ = m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			chatRows := lipgloss.Height(m.View())
			app := m.(AppModel)
			app.viewMode = "sessions"
			panel := NewSessionPanelModel(workDir).SetWidth(sz.w).SetHeight(sz.h)
			app.sessionPanel = &panel
			view := app.View()
			assertFitsWidth(t, view, sz.w)
			if got := lipgloss.Height(view); got != chatRows {
				t.Fatalf("sessions=%v %dx%d: the picker view is %d rows, the chat view %d:\n%s", withSessions, sz.w, sz.h, got, chatRows, stripANSI(view))
			}
		}
	}
}
