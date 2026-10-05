package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// fitWidth is the TUI's one truncation rule: it cuts s, which may carry
// ANSI styling, to at most w terminal cells and ends it with "…" when
// anything was cut, so a panel row never wraps. It counts cells (a CJK
// character or an emoji is two) and never splits a rune. Each line of a
// multi-line s is fitted on its own.
func fitWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if strings.Contains(s, "\n") {
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			lines[i] = ansi.Truncate(l, w, "…")
		}
		return strings.Join(lines, "\n")
	}
	return ansi.Truncate(s, w, "…")
}

// elideLeft is fitWidth cutting from the left: it keeps the end of s (a
// file path's name) and starts it with "…" when anything was cut.
func elideLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	n := ansi.StringWidth(s)
	if n <= w {
		return s
	}
	// TruncateLeft keeps a wide character the cut lands inside, so cut
	// one more cell until the rest fits.
	for k := n - w + 1; k <= n; k++ {
		if rest := ansi.TruncateLeft(s, k, ""); ansi.StringWidth(rest) <= w-1 {
			return "…" + rest
		}
	}
	return "…"
}

// padRight fits s to w cells and pads it with spaces to exactly w.
func padRight(s string, w int) string {
	s = fitWidth(s, w)
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}
