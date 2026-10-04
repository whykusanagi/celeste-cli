package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// fitWidth cuts s, which may carry ANSI styling, to at most w cells and ends
// it with "…" when anything was cut, so a panel row never wraps.
func fitWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return lipgloss.NewStyle().MaxWidth(w-1).Render(s) + "…"
}

// padRight fits s to w cells and pads it with spaces to exactly w.
func padRight(s string, w int) string {
	s = fitWidth(s, w)
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}
