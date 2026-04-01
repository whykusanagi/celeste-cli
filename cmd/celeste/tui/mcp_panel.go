// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the MCP server status panel, accessed via /mcp command.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MCPPanelModel displays MCP server connection status and tool counts.
type MCPPanelModel struct {
	active  bool
	servers []MCPServerInfo
	cursor  int
	width   int
	height  int
}

// NewMCPPanelModel creates a new MCP panel model.
func NewMCPPanelModel() MCPPanelModel {
	return MCPPanelModel{}
}

// Init initializes the model (no-op).
func (m MCPPanelModel) Init() tea.Cmd {
	return nil
}

// SetSize updates the component dimensions.
func (m *MCPPanelModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Active returns whether the MCP panel is currently displayed.
func (m MCPPanelModel) Active() bool {
	return m.active
}

// Show activates the MCP panel.
func (m *MCPPanelModel) Show() {
	m.active = true
	m.cursor = 0
}

// Update handles messages for the MCP panel.
func (m MCPPanelModel) Update(msg tea.Msg) (MCPPanelModel, tea.Cmd) {
	switch msg := msg.(type) {
	case MCPStatusMsg:
		m.servers = msg.Servers

	case tea.KeyMsg:
		if !m.active {
			break
		}
		switch msg.String() {
		case "esc", "q":
			m.active = false
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.servers)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

// View renders the MCP panel.
func (m MCPPanelModel) View() string {
	if !m.active {
		return ""
	}

	w := m.width
	if w < 40 {
		w = 44
	}
	innerW := w - 6
	if innerW < 30 {
		innerW = 30
	}

	borderStyle := lipgloss.NewStyle().Foreground(ColorBorderPurple)
	titleStyle := lipgloss.NewStyle().Foreground(ColorPurpleNeon).Bold(true)
	connectedStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	disconnectedStyle := lipgloss.NewStyle().Foreground(ColorError)
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	infoStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)
	cursorStyle := lipgloss.NewStyle().Foreground(ColorAccentGlow).Bold(true)
	footerStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)

	hRule := strings.Repeat("─", innerW)
	title := " MCP Servers "

	// Top border
	topFill := innerW - len(title)
	if topFill < 0 {
		topFill = 0
	}
	top := borderStyle.Render("╭─") + titleStyle.Render(title) + borderStyle.Render(strings.Repeat("─", topFill)+"╮")

	// Server lines
	var lines []string
	lines = append(lines, top)

	totalTools := 0
	for i, srv := range m.servers {
		var dot string
		var detail string
		if srv.Connected {
			dot = connectedStyle.Render("●")
			detail = fmt.Sprintf("%d tools    %s", srv.ToolCount, srv.Transport)
			totalTools += srv.ToolCount
		} else {
			dot = disconnectedStyle.Render("○")
			detail = "disconnected"
		}

		prefix := "  "
		srvName := nameStyle.Render(srv.Name)
		if i == m.cursor {
			prefix = cursorStyle.Render("> ")
			srvName = cursorStyle.Render(srv.Name)
		}

		line := fmt.Sprintf("%s%s %s  %s  %s",
			borderStyle.Render("│"),
			prefix,
			dot,
			srvName,
			infoStyle.Render(detail),
		)
		lines = append(lines, line)
	}

	if len(m.servers) == 0 {
		lines = append(lines, borderStyle.Render("│")+"  "+infoStyle.Render("No MCP servers configured"))
	}

	// Blank + summary line
	lines = append(lines, borderStyle.Render("│"))
	summaryText := fmt.Sprintf("  Total: %d external tools available", totalTools)
	lines = append(lines, borderStyle.Render("│")+infoStyle.Render(summaryText))

	// Bottom border
	bot := borderStyle.Render("╰" + hRule + "──╯")
	lines = append(lines, bot)

	// Footer with keybindings
	lines = append(lines, footerStyle.Render("[↑/↓] Navigate  [Esc] Close"))

	return strings.Join(lines, "\n")
}
