package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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
