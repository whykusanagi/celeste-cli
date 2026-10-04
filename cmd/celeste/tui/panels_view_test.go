package tui

import (
	"errors"
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

// V10: a tool description with blank lines (code_review's) stays on its
// row in the skills browser; the browser keeps its height and width.
func TestSkillsBrowserMultilineDescriptionKeepsItsRow(t *testing.T) {
	skills := []SkillDefinition{
		{Name: "code_review", Description: "Automated code review using structural graph analysis.\n\nAnalyzes every function in the codebase\tfor stubs."},
		{Name: "base64_decode", Description: "Decode a base64 string"},
		{Name: "weather", Description: "Récupère la météo — prévisions détaillées pour une ville donnée, avec vent, humidité et alertes régionales"},
	}
	for _, sz := range auditSizes {
		var m tea.Model = NewSkillsBrowserModel(skills)
		m, _ = m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		view := m.View()
		assertFitsWidth(t, view, sz.w)
		if got := lipgloss.Height(view); got > sz.h {
			t.Fatalf("%dx%d: the browser is %d rows:\n%s", sz.w, sz.h, got, stripANSI(view))
		}
		lines := strings.Split(stripANSI(view), "\n")
		for i, ln := range lines {
			if !strings.Contains(ln, "code_review") {
				continue
			}
			if !strings.Contains(ln, "structural graph") || !strings.Contains(lines[i+1], "weather") {
				t.Fatalf("%dx%d: code_review's description broke its row:\n%s", sz.w, sz.h, stripANSI(view))
			}
			if sz.w >= 120 && !strings.Contains(ln, "analysis. Analyzes every") {
				t.Fatalf("%dx%d: the blank line was not collapsed: %q", sz.w, sz.h, ln)
			}
		}
	}
}

// V11: the /memories empty state is three left-aligned lines; none drifts
// right or runs past 80 columns.
func TestMemoriesEmptyStateLinesUpAndFits(t *testing.T) {
	isolatedHome(t)
	for _, sz := range auditSizes {
		var m tea.Model = NewMemoryManagerModel(t.TempDir())
		m, _ = m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		view := m.View()
		assertFitsWidth(t, view, sz.w)
		plain := stripANSI(view)
		for _, want := range []string{
			"No memories saved for this project.",
			"Celeste will save memories automatically during conversation.",
			`Or use: celeste remember "<text>"`,
		} {
			found := false
			for _, ln := range strings.Split(plain, "\n") {
				if strings.HasPrefix(ln, "  "+want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("%dx%d: no row starts with %q at column 2:\n%s", sz.w, sz.h, want, plain)
			}
		}
	}
}

// V4: the header stays one row (plus its rule) at 80 columns with a long
// model name or in NSFW mode; what does not fit is cut with "…".
func TestHeaderFitsWithLongModelAndNSFW(t *testing.T) {
	long := NewHeaderModel().SetEndpoint("sakana").SetModel("nonexistent-model-xyz").SetSkillsEnabled(true).SetContextUsage(19_400, 1_000_000)
	huge := NewHeaderModel().SetEndpoint("openrouter").SetModel("some-vendor/an-extraordinarily-long-model-identifier-preview-2026-10-01").SetSkillsEnabled(true).SetContextUsage(0, 1_000_000)
	nsfw := NewHeaderModel().SetNSFWMode(true).SetImageModel("lustify-sdxl").SetContextUsage(31_700, 1_000_000)
	for _, sz := range auditSizes {
		for name, h := range map[string]HeaderModel{"long": long, "huge": huge, "nsfw": nsfw} {
			view := h.SetWidth(sz.w).View()
			assertFitsWidth(t, view, sz.w)
			if got := lipgloss.Height(view); got != 2 {
				t.Fatalf("%s %dx%d: the header is %d rows:\n%s", name, sz.w, sz.h, got, stripANSI(view))
			}
			plain := stripANSI(view)
			if !strings.Contains(plain, "Celeste CLI") {
				t.Fatalf("%s %dx%d: the header lost its title:\n%s", name, sz.w, sz.h, plain)
			}
		}
	}
	if plain := stripANSI(long.SetWidth(80).View()); !strings.Contains(plain, "nonexistent-model-xyz") {
		t.Fatalf("at 80 columns the model name should still fit once the exit hint goes:\n%s", plain)
	}
	if plain := stripANSI(huge.SetWidth(80).View()); !strings.Contains(plain, "…") {
		t.Fatalf("a model name too long for the row should end in …:\n%s", plain)
	}
}

// openAI404 is the error go-openai returns for an unknown model.
var openAI404 = errors.New(`error, status code: 404, status: 404 Not Found, message: Model "nonexistent-model-xyz" not found`)

// V5: a long error stays on the status bar's one row at 80 columns, cut
// with "…", and reads "Error: status code: 404…", not "Error: error, …".
func TestStatusBarLongErrorIsOneRow(t *testing.T) {
	for _, sz := range auditSizes {
		var m tea.Model = NewApp(nil)
		m, _ = m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m, _ = m.Update(StreamErrorMsg{Err: openAI404})
		app := m.(AppModel)
		bar := app.status.View()
		assertFitsWidth(t, bar, sz.w)
		if got := lipgloss.Height(bar); got != 1 {
			t.Fatalf("%dx%d: the status bar is %d rows:\n%s", sz.w, sz.h, got, stripANSI(bar))
		}
		plain := stripANSI(bar)
		if strings.Contains(plain, "Error: error") || !strings.Contains(plain, "Error: status code: 404") {
			t.Fatalf("%dx%d: status bar text: %q", sz.w, sz.h, plain)
		}
		if sz.w == 80 && !strings.HasSuffix(strings.TrimRight(plain, " "), "…") {
			t.Fatalf("%dx%d: a cut error should end in …: %q", sz.w, sz.h, plain)
		}
		if got := lipgloss.Height(app.View()); got > sz.h {
			t.Fatalf("%dx%d: the view is %d rows:\n%s", sz.w, sz.h, got, stripANSI(app.View()))
		}
	}
	// A multi-line status (a warning, a streaming phrase) is one row too.
	bar := NewStatusModel().SetWidth(80).SetText("line one\nline two").View()
	if got := lipgloss.Height(bar); got != 1 {
		t.Fatalf("a two-line status text took %d rows: %q", got, stripANSI(bar))
	}
}
