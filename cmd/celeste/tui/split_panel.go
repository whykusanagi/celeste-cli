package tui

import (
	"fmt"
	"slices"
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
// incrementing the offset so the same lines stay visible.
func (s *SplitPanel) AddAction(text string) {
	s.actions = append(s.actions, text)
	if len(s.actions) > maxActionEntries {
		trimmed := len(s.actions) - maxActionEntries
		s.actions = s.actions[trimmed:]
		if s.scrollOffset > 0 {
			s.scrollOffset -= trimmed
			if s.scrollOffset < 0 {
				s.scrollOffset = 0
			}
		}
	}
	// If scrolled up, bump offset by 1 so the viewport doesn't jump on new entries.
	if s.scrollOffset > 0 {
		s.scrollOffset++
	}
}

// Actions returns the current action feed entries.
func (s *SplitPanel) Actions() []string { return s.actions }

// ScrollUp scrolls the left action feed toward older entries (up = back in time).
func (s *SplitPanel) ScrollUp(lines int) {
	s.scrollOffset += lines
	maxOffset := len(s.actions) - 1
	if s.scrollOffset > maxOffset {
		s.scrollOffset = maxOffset
	}
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
}

// ScrollDown scrolls toward the latest entries. Reaching 0 resumes auto-follow.
func (s *SplitPanel) ScrollDown(lines int) {
	s.scrollOffset -= lines
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
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

	// Reserve: 1 for header, 1 for optional scroll indicator.
	maxLines := contentH - 2
	scrollHintNeeded := s.scrollOffset > 0
	if scrollHintNeeded {
		maxLines-- // room for scroll hint at bottom
	}
	if maxLines < 1 {
		maxLines = 1
	}

	end := len(s.actions) - s.scrollOffset
	if end < 0 {
		end = 0
	}
	start := end - maxLines
	if start < 0 {
		start = 0
	}
	entries := s.actions[start:end]

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = fitLine("● "+e, width)
	}

	result := header + "\n" + strings.Join(lines, "\n")
	if scrollHintNeeded {
		remaining := len(s.actions) - end
		hint := fmt.Sprintf("↑ %d older  ↓ pgdn/↓ to resume", s.scrollOffset)
		if remaining > 0 {
			hint = fmt.Sprintf("↑ %d older  ↓ %d newer", s.scrollOffset, remaining)
		}
		result += "\n" + dimStyle.Render(hint)
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
		maxLines := contentH - 2
		if maxLines < 1 {
			maxLines = 1
		}
		start := s.rightScroll
		if start > len(allLines)-1 {
			start = len(allLines) - 1
		}
		if start < 0 {
			start = 0
		}
		end := start + maxLines
		if end > len(allLines) {
			end = len(allLines)
		}
		visible := allLines[start:end]
		trimmed := make([]string, len(visible))
		for i, l := range visible {
			trimmed[i] = fitLine(l, width)
		}
		result := header + "\n" + strings.Join(trimmed, "\n")
		if s.rightScroll > 0 || end < len(allLines) {
			result += "\n" + dimStyle.Render(fmt.Sprintf("line %d-%d / %d", start+1, end, len(allLines)))
		}
		return result
	}
	if s.diffFile == "" {
		return dimStyle.Render("(waiting for agent response...)")
	}

	header := headerStyle.Render(s.diffFile)
	// Split diff into lines and apply scroll.
	allLines := strings.Split(s.diffContent, "\n")
	maxLines := contentH - 2 // header + 1 pad
	if maxLines < 1 {
		maxLines = 1
	}

	start := s.rightScroll
	if start > len(allLines)-1 {
		start = len(allLines) - 1
	}
	if start < 0 {
		start = 0
	}
	end := start + maxLines
	if end > len(allLines) {
		end = len(allLines)
	}
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
	if s.rightScroll > 0 || end < len(allLines) {
		hint := fmt.Sprintf("line %d-%d / %d", start+1, end, len(allLines))
		result += "\n" + dimStyle.Render(hint)
	}
	return result
}

// viewNarrow is the feed alone, its latest entries, for a terminal under
// 40 columns: still no wider or taller than the panel.
func (s *SplitPanel) viewNarrow() string {
	rows := max(s.height, 1)
	// Cap rows, not entries: an entry may hold several lines (a verdict).
	var lines []string
	for i := len(s.actions) - 1; i >= 0 && len(lines) < rows; i-- {
		parts := strings.Split(s.actions[i], "\n")
		for j := len(parts) - 1; j >= 0 && len(lines) < rows; j-- {
			lines = append(lines, fitLine(parts[j], s.width))
		}
	}
	slices.Reverse(lines)
	return strings.Join(lines, "\n")
}
