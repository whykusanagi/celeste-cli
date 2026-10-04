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

// menuItems are the commands /menu offers. Selecting one sends "/<name>"
// with no arguments, so each must do something useful bare; exit quits
// (TestMenuListsKnownAnd20Commands). The menu has no
// scrolling, so the list stays within a 24-row terminal; /help lists the
// rest.
var menuItems = []MenuItem{
	{"help", "Show available commands"},
	{"clear", "Clear chat history"},
	{"session", "Pick a saved session"},
	{"plan", "Plan mode: read-only tools until you approve a plan"},
	{"diff", "List the files this session changed"},
	{"undo", "Undo the last file change"},
	{"rewind", "Take back the last prompt and its file changes"},
	{"fork", "Continue in a copy of this session"},
	{"compact", "Summarize older history to free context"},
	{"handoff", "Summarize this session into a new one"},
	{"context", "Show context/token usage"},
	{"costs", "Show session costs"},
	{"memories", "Manage project memories"},
	{"grimoire", "Show the project grimoire"},
	{"index", "Show code graph status"},
	{"tools", "Browse available tools"},
	{"mcp", "Show MCP servers and tools"},
	{"persona", "Personality sliders"},
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
