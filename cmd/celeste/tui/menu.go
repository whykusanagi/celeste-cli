package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MenuModel is the TUI model for interactive command menu
type MenuModel struct {
	cursor        int
	width, height int
}

// MenuItem represents a command in the menu
type MenuItem struct {
	Name        string
	Description string
}

var menuItems = []MenuItem{
	{"help", "Show available commands"},
	{"clear", "Clear chat history"},
	{"config", "Show current configuration"},
	{"tools", "Show available skills"},
	{"skills", "Show available skills (interactive)"},
	{"collections", "Manage Collections (xAI RAG)"},
	{"menu", "Show this command menu"},
	{"context", "Show context/token usage"},
	{"stats", "Show usage statistics"},
	{"session", "Manage conversation sessions"},
	{"export", "Export session data"},
	{"providers", "List AI providers"},
	{"exit", "Exit the application"},
}

// NewMenuModel creates a new menu model
func NewMenuModel() MenuModel {
	return MenuModel{}
}

// Init initializes the model
func (m MenuModel) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m MenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(menuItems)-1 {
				m.cursor++
			}
		case "enter", " ":
			// User selected a command - return it
			// The parent will handle executing the command
			return m, func() tea.Msg {
				return menuItemSelectedMsg{
					command: menuItems[m.cursor].Name,
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

type menuItemSelectedMsg struct {
	command string
}

// View renders the model
func (m MenuModel) View() string {
	var content string

	// Header
	content += lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#8b5cf6")). // Purple (corrupted theme)
		Render("Available Commands") + "\n\n"

	// Menu items
	for i, item := range menuItems {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		line := fmt.Sprintf("%s%-15s  %s", cursor, item.Name, item.Description)
		if i == m.cursor {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d946ef")). // Bright pink (corrupted theme - cursor)
				Bold(true).
				Render(line)
		}
		content += line + "\n"
	}

	// Footer with keybindings
	footer := "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6d28d9")). // Dark purple (corrupted theme - muted)
		Render("[↑/↓/k/j] Navigate  [Enter/Space] Select  [Q/Esc] Back to Chat")

	return content + footer
}
