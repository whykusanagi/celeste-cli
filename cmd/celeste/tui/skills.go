package tui

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SkillsBrowserModel is the TUI model for interactive skills browser
type SkillsBrowserModel struct {
	cursor        int
	skillsList    []SkillDefinition
	width, height int
}

// NewSkillsBrowserModel creates a new skills model
func NewSkillsBrowserModel(skillsList []SkillDefinition) SkillsBrowserModel {
	// Sort by name for consistent ordering
	sort.Slice(skillsList, func(i, j int) bool {
		return skillsList[i].Name < skillsList[j].Name
	})

	return SkillsBrowserModel{
		skillsList: skillsList,
	}
}

// Init initializes the model
func (m SkillsBrowserModel) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m SkillsBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "esc":
			// Return to chat
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.skillsList)-1 {
				m.cursor++
			}
		case "enter", " ":
			// User selected a skill - return it
			return m, func() tea.Msg {
				return skillSelectedMsg{
					skillName: m.skillsList[m.cursor].Name,
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

type skillSelectedMsg struct {
	skillName string
}

// View renders the model
func (m SkillsBrowserModel) View() string {
	var content string

	// Header
	content += lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#8b5cf6")). // Purple (corrupted theme)
		Render(fmt.Sprintf("Available Skills (%d)", len(m.skillsList))) + "\n\n"

	// List all skills
	for i, skill := range m.skillsList {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		// Truncate description if too long
		desc := skill.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}

		line := fmt.Sprintf("%s%-25s  %s", cursor, skill.Name, desc)
		if i == m.cursor {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d946ef")). // Bright pink (corrupted theme - cursor)
				Bold(true).
				Render(line)
		} else {
			// Show skill name in purple for non-selected items
			namePart := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b5cf6")).
				Render(fmt.Sprintf("%s%-25s", cursor, skill.Name))
			descPart := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6d28d9")).
				Render("  " + desc)
			line = namePart + descPart
		}
		content += line + "\n"
	}

	// Footer with keybindings and tip
	footer := "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7c3aed")). // Dark purple
		Render("💡 TIP: Some skills require parameters. Add them in chat after selection.") + "\n"

	footer += lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6d28d9")). // Dark purple (corrupted theme - muted)
		Render("[↑/↓/k/j] Navigate  [Enter/Space] Select  [Q/Esc] Back to Chat")

	return content + footer
}
