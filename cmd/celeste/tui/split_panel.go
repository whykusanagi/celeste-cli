package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const maxActionEntries = 200

// SplitPanel renders a two-column layout:
//
//	Left  — agent action feed (scrollable log)
//	Right — live code output / file diff / verdict
type SplitPanel struct {
	width        int
	height       int
	actions      []string
	diffFile     string
	diffContent  string
	verdict      string
	output       string // live response text, updated each turn
	scrollOffset int    // lines from the bottom (0 = auto-follow latest)
	rightScroll  int    // scroll offset for right panel content
}

// NewSplitPanel creates a SplitPanel sized to the given terminal dimensions.
func NewSplitPanel(width, height int) *SplitPanel {
	return &SplitPanel{width: width, height: height}
}

// Resize updates the panel dimensions (call on tea.WindowSizeMsg).
func (s *SplitPanel) Resize(width, height int) {
	s.width = width
	s.height = height
}

// AddAction appends an entry to the left action feed.
// When the user has scrolled up (scrollOffset > 0), the view is pinned by
// raising the offset by the entry's rows so the same lines stay visible.
func (s *SplitPanel) AddAction(text string) {
	s.actions = append(s.actions, text)
	if len(s.actions) > maxActionEntries {
		trimmed := len(s.actions) - maxActionEntries
		s.actions = s.actions[trimmed:]
	}
	if s.scrollOffset > 0 {
		s.scrollOffset += entryRows(text)
		s.scrollOffset = min(s.scrollOffset, s.maxScroll())
	}
}

// Actions returns the current action feed entries.
func (s *SplitPanel) Actions() []string { return s.actions }

// entryRows is how many feed rows an entry takes: one per line.
func entryRows(e string) int { return strings.Count(e, "\n") + 1 }

// feedLines is the action feed as display rows: an entry's first line
// marked "● ", its further lines indented under it.
func (s *SplitPanel) feedLines() []string {
	var out []string
	for _, e := range s.actions {
		for i, l := range strings.Split(e, "\n") {
			if i == 0 {
				out = append(out, "● "+l)
			} else {
				out = append(out, "  "+l)
			}
		}
	}
	return out
}

// feedRowsAvailable is the rows the feed has under its header at the
// panel's current size (see View and viewNarrow).
func (s *SplitPanel) feedRowsAvailable() int {
	if s.width < 40 {
		return max(s.height, 1)
	}
	return max(s.height, 3) - 2 - 1 // borders, header
}

// feedWindow is the slice of total feed rows shown in avail rows at the
// current scroll offset: rows [start, end), and whether a scroll marker
// takes the last row. A feed that fits is shown whole, unscrolled (#353).
func (s *SplitPanel) feedWindow(total, avail int, marker bool) (start, end int, scrolls bool) {
	if total <= avail {
		return 0, total, false
	}
	rows := avail
	if marker {
		rows = max(avail-1, 1)
	}
	offset := min(max(s.scrollOffset, 0), total-rows)
	end = total - offset
	return end - rows, end, true
}

// maxScroll is the largest useful scroll offset at the panel's current
// size: the one showing the feed's first page.
func (s *SplitPanel) maxScroll() int {
	avail := s.feedRowsAvailable()
	total := len(s.feedLines())
	if total <= avail {
		return 0
	}
	if s.width >= 40 {
		avail = max(avail-1, 1) // the marker row
	}
	return total - avail
}

// ScrollUp scrolls the left action feed toward older entries (up = back in
// time), no further than its first page.
func (s *SplitPanel) ScrollUp(lines int) {
	s.scrollOffset = min(max(s.scrollOffset+lines, 0), s.maxScroll())
}

// ScrollDown scrolls toward the latest entries. Reaching 0 resumes auto-follow.
func (s *SplitPanel) ScrollDown(lines int) {
	s.scrollOffset = min(s.scrollOffset, s.maxScroll())
	s.scrollOffset -= lines
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
}

// rightLines is the scrollable content of the right pane, the live output
// or the diff; nil when it shows a verdict or is waiting.
func (s *SplitPanel) rightLines() []string {
	switch {
	case s.verdict != "":
		return nil
	case s.diffFile != "":
		return strings.Split(s.diffContent, "\n")
	case s.output != "":
		return strings.Split(s.output, "\n")
	}
	return nil
}

// rightPageRows is how many content lines the right pane shows: its inner
// rows less the header and the "line x-y / n" marker.
func rightPageRows(contentH int) int { return max(contentH-2, 1) }

// rightWindow is the [start, end) of total right-pane lines shown in a page
// of rows: the last page is a full one (#353).
func (s *SplitPanel) rightWindow(total, rows int) (start, end int) {
	start = min(max(s.rightScroll, 0), max(total-rows, 0))
	return start, min(start+rows, total)
}

// ScrollRight pages the right pane by delta lines (negative = up), within
// its content.
func (s *SplitPanel) ScrollRight(delta int) {
	rows := rightPageRows(max(s.height, 3) - 2)
	s.rightScroll = min(max(s.rightScroll+delta, 0), max(len(s.rightLines())-rows, 0))
}

// AtBottom reports whether the left panel is auto-following the latest entry.
func (s *SplitPanel) AtBottom() bool { return s.scrollOffset == 0 }

// SetDiff updates the right panel with a file diff.
func (s *SplitPanel) SetDiff(file, diff string) {
	s.diffFile = file
	s.diffContent = diff
	s.verdict = ""
	s.rightScroll = 0
}

// SetVerdict replaces the right panel with a verdict report.
func (s *SplitPanel) SetVerdict(text string) {
	s.verdict = text
	s.diffFile = ""
	s.diffContent = ""
	s.rightScroll = 0
}

// SetOutput updates the live response text shown in the right panel.
// Called after each agent turn to display what the model responded.
func (s *SplitPanel) SetOutput(text string) {
	if text == "" {
		return
	}
	s.output = text
	// Reset right scroll so new content starts at the top.
	s.rightScroll = 0
}

// AppendOutput appends text to the right panel without resetting scroll.
// Used for incremental updates (e.g., tool calls building up during review).
func (s *SplitPanel) AppendOutput(text string) {
	s.output += text
}

// DiffFile returns the file name currently shown in the right panel.
func (s *SplitPanel) DiffFile() string { return s.diffFile }

// DiffContent returns the diff currently shown in the right panel.
func (s *SplitPanel) DiffContent() string { return s.diffContent }

// View renders the split panel as a string for Bubble Tea: exactly width
// columns by height rows (at least 3), whatever the panes hold. Lines too
// wide for a pane are cut with "…", never wrapped, and a pane shows only
// the rows it has (V21: an overflowing pane pushed the view off screen).
func (s *SplitPanel) View() string {
	if s.width < 40 {
		return s.viewNarrow()
	}
	h := max(s.height, 3)
	leftW := s.width / 2
	rightW := s.width - leftW
	// Each pane: a 1-column border and a 1-column pad on either side.
	left := s.renderActionFeed(leftW-4, h-2)
	right := s.renderArtifact(rightW-4, h-2)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		splitPane(left, leftW, h, "#8b5cf6"),
		splitPane(right, rightW, h, "#00d4ff"),
	)
}

// splitPane boxes content in a rounded border exactly w columns by h rows.
func splitPane(content string, w, h int, border string) string {
	inner, rows := w-4, h-2
	lines := strings.Split(content, "\n")
	if len(lines) > rows {
		lines = lines[:rows]
	}
	for i, l := range lines {
		lines[i] = fitLine(l, inner)
	}
	return lipgloss.NewStyle().
		Width(w-2). // lipgloss widths include the padding, not the border
		Height(rows).
		MaxHeight(h).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(border)).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

// fitLine makes l one terminal row at most width columns wide: tabs become
// spaces, carriage returns go, and the rest is cut with "…".
func fitLine(l string, width int) string {
	l = strings.ReplaceAll(strings.ReplaceAll(l, "\t", "    "), "\r", "")
	if width < 1 {
		return ""
	}
	return ansi.Truncate(l, width, "…")
}

func (s *SplitPanel) renderActionFeed(width, contentH int) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#d94f90"))
	header := headerStyle.Render("AGENT ACTIONS")
	if len(s.actions) == 0 {
		return header + "\n(waiting...)"
	}

	// The header takes a row; when the feed does not fit, a marker counting
	// the rows hidden above and below takes the last one (#353).
	all := s.feedLines()
	start, end, scrolls := s.feedWindow(len(all), max(contentH-1, 1), true)
	lines := make([]string, 0, end-start)
	for _, l := range all[start:end] {
		lines = append(lines, fitLine(l, width))
	}
	result := header + "\n" + strings.Join(lines, "\n")
	if scrolls {
		var parts []string
		if start > 0 {
			parts = append(parts, fmt.Sprintf("↑ %d older", start))
		}
		if newer := len(all) - end; newer > 0 {
			parts = append(parts, fmt.Sprintf("↓ %d newer", newer))
		}
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))
		result += "\n" + dimStyle.Render(fitLine(strings.Join(parts, "  "), width))
	}
	return result
}

func (s *SplitPanel) renderArtifact(width, contentH int) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00d4ff"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))

	if s.verdict != "" {
		header := headerStyle.Render("REVIEW VERDICT")
		return header + "\n" + s.verdict
	}
	if s.diffFile == "" && s.output != "" {
		// Show live response output with scroll support
		header := headerStyle.Render("code output")
		allLines := strings.Split(s.output, "\n")
		start, end := s.rightWindow(len(allLines), rightPageRows(contentH))
		visible := allLines[start:end]
		trimmed := make([]string, len(visible))
		for i, l := range visible {
			trimmed[i] = fitLine(l, width)
		}
		result := header + "\n" + strings.Join(trimmed, "\n")
		if start > 0 || end < len(allLines) {
			result += "\n" + dimStyle.Render(fitLine(rightMarker(start, end, len(allLines)), width))
		}
		return result
	}
	if s.diffFile == "" {
		return dimStyle.Render("(waiting for agent response...)")
	}

	header := headerStyle.Render(s.diffFile)
	// Split diff into lines and apply scroll.
	allLines := strings.Split(s.diffContent, "\n")
	start, end := s.rightWindow(len(allLines), rightPageRows(contentH))
	visible := allLines[start:end]

	// Color diff lines: additions green, removals red, header cyan.
	colored := make([]string, len(visible))
	for i, l := range visible {
		l = fitLine(l, width)
		switch {
		case strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---"):
			colored[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("#00d4ff")).Render(l)
		case strings.HasPrefix(l, "+"):
			colored[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Render(l)
		case strings.HasPrefix(l, "-"):
			colored[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Render(l)
		case strings.HasPrefix(l, "@@"):
			colored[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa")).Render(l)
		default:
			colored[i] = l
		}
	}

	result := header + "\n" + strings.Join(colored, "\n")
	if start > 0 || end < len(allLines) {
		result += "\n" + dimStyle.Render(fitLine(rightMarker(start, end, len(allLines)), width))
	}
	return result
}

// rightMarker is the right pane's position marker and its paging keys.
func rightMarker(start, end, total int) string {
	return fmt.Sprintf("line %d-%d / %d · ctrl/alt+↑/↓ scroll", start+1, end, total)
}

// viewNarrow is the feed alone for a terminal under 40 columns: the rows
// at the current scroll offset (its latest rows when following), still no
// wider or taller than the panel.
func (s *SplitPanel) viewNarrow() string {
	rows := max(s.height, 1)
	// Cap rows, not entries: an entry may hold several lines (a verdict).
	var all []string
	for _, e := range s.actions {
		all = append(all, strings.Split(e, "\n")...)
	}
	start, end, _ := s.feedWindow(len(all), rows, false)
	lines := make([]string, 0, end-start)
	for _, l := range all[start:end] {
		lines = append(lines, fitLine(l, s.width))
	}
	return strings.Join(lines, "\n")
}
