package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const maxActionEntries = 200

// SplitPanel renders a two-column layout:
//
//	Left  — agent action feed (scrollable log)
//	Right — file diff or artifact view
type SplitPanel struct {
	width       int
	height      int
	actions     []string
	diffFile    string
	diffContent string
	verdict     string
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
func (s *SplitPanel) AddAction(text string) {
	s.actions = append(s.actions, text)
	if len(s.actions) > maxActionEntries {
		s.actions = s.actions[len(s.actions)-maxActionEntries:]
	}
}

// Actions returns the current action feed entries.
func (s *SplitPanel) Actions() []string { return s.actions }

// SetDiff updates the right panel with a file diff.
func (s *SplitPanel) SetDiff(file, diff string) {
	s.diffFile = file
	s.diffContent = diff
	s.verdict = ""
}

// SetVerdict replaces the right panel with a verdict report.
func (s *SplitPanel) SetVerdict(text string) {
	s.verdict = text
	s.diffFile = ""
	s.diffContent = ""
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
	rightContent := s.renderArtifact(rightWidth - 4)

	return lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(leftContent),
		rightStyle.Render(rightContent),
	)
}

func (s *SplitPanel) renderActionFeed(width, contentH int) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#d94f90")).Render("AGENT ACTIONS")
	if len(s.actions) == 0 {
		return header + "\n(waiting...)"
	}
	// contentH is the lipgloss box content area; subtract header line and padding.
	maxLines := contentH - 2
	if maxLines < 1 {
		maxLines = 1
	}
	entries := s.actions
	if len(entries) > maxLines {
		entries = entries[len(entries)-maxLines:]
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		line := "● " + e
		if len(line) > width {
			line = line[:width-1] + "…"
		}
		lines[i] = line
	}
	return header + "\n" + strings.Join(lines, "\n")
}

func (s *SplitPanel) renderArtifact(width int) string {
	if s.verdict != "" {
		header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00d4ff")).Render("REVIEW VERDICT")
		return header + "\n" + s.verdict
	}
	if s.diffFile == "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Render("(no file changes yet)")
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00d4ff")).Render(s.diffFile)
	return header + "\n" + s.diffContent
}

func (s *SplitPanel) viewNarrow() string {
	return strings.Join(s.actions, "\n")
}
