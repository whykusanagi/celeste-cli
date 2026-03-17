package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

// ScrollRightUp scrolls the right artifact panel up.
func (s *SplitPanel) ScrollRightUp(lines int) {
	s.rightScroll += lines
}

// ScrollRightDown scrolls the right artifact panel down.
func (s *SplitPanel) ScrollRightDown(lines int) {
	s.rightScroll -= lines
	if s.rightScroll < 0 {
		s.rightScroll = 0
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

// View renders the split panel as a string for Bubble Tea.
func (s *SplitPanel) View() string {
	if s.width < 40 {
		return s.viewNarrow()
	}

	leftWidth := s.width / 2
	rightWidth := s.width - leftWidth - 1

	// s.height is the available panel height (total terminal minus header/status/input).
	// lipgloss Height sets content area; the border adds 2 lines (top + bottom).
	contentH := s.height - 2
	if contentH < 3 {
		contentH = 3
	}

	leftStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(contentH).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#8b5cf6")).
		Padding(0, 1)

	rightStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Height(contentH).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#00d4ff")).
		Padding(0, 1)

	leftContent := s.renderActionFeed(leftWidth-4, contentH)
	rightContent := s.renderArtifact(rightWidth-4, contentH)

	return lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(leftContent),
		rightStyle.Render(rightContent),
	)
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
		line := "● " + e
		if len([]rune(line)) > width {
			runes := []rune(line)
			line = string(runes[:width-1]) + "…"
		}
		lines[i] = line
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
			if width > 0 && len([]rune(l)) > width {
				l = string([]rune(l)[:width-1]) + "…"
			}
			trimmed[i] = l
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
		if width > 0 && len([]rune(l)) > width {
			l = string([]rune(l)[:width-1]) + "…"
		}
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

func (s *SplitPanel) viewNarrow() string {
	return strings.Join(s.actions, "\n")
}
