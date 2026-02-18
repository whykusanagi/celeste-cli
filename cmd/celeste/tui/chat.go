// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the chat panel component with scrollable viewport.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ChatModel represents the chat panel with scrollable messages.
type ChatModel struct {
	viewport       viewport.Model
	messages       []ChatMessage
	functionCalls  []FunctionCall
	width          int
	height         int
	ready          bool
	userScrolled   bool // Track if user has scrolled manually
	showSkillCalls bool // Toggle to show/hide skill call logs
}

// NewChatModel creates a new chat model.
func NewChatModel() ChatModel {
	return ChatModel{
		messages:       []ChatMessage{},
		functionCalls:  []FunctionCall{},
		showSkillCalls: false, // Hidden by default for cleaner UI
	}
}

// SetSize sets the chat panel size.
func (m ChatModel) SetSize(width, height int) ChatModel {
	m.width = width
	m.height = height

	// Account for padding (1 on each side = 2 total)
	viewWidth := width - 2
	if viewWidth < 10 {
		viewWidth = 10
	}

	if !m.ready {
		m.viewport = viewport.New(viewWidth, height)
		m.viewport.YPosition = 0
		m.ready = true
	} else {
		m.viewport.Width = viewWidth
		m.viewport.Height = height
	}

	m.updateContent()
	return m
}

// Init implements the Init method for ChatModel (partial tea.Model).
func (m ChatModel) Init() tea.Cmd {
	return nil
}

// Update handles messages for the chat panel.
func (m ChatModel) Update(msg tea.Msg) (ChatModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "pgup":
			m.viewport.PageUp()
			m.userScrolled = true
		case "pgdown":
			m.viewport.PageDown()
			// If at bottom, reset userScrolled
			if m.viewport.AtBottom() {
				m.userScrolled = false
			}
		case "shift+up":
			m.viewport.ScrollUp(3)
			m.userScrolled = true
		case "shift+down":
			m.viewport.ScrollDown(3)
			if m.viewport.AtBottom() {
				m.userScrolled = false
			}
		case "end":
			m.viewport.GotoBottom()
			m.userScrolled = false
		case "home":
			m.viewport.GotoTop()
			m.userScrolled = true
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// View renders the chat panel.
func (m ChatModel) View() string {
	if !m.ready {
		return "\n  Loading chat..."
	}

	return ChatPanelStyle.
		Width(m.width).
		Height(m.height).
		Render(m.viewport.View())
}

// AddUserMessage adds a user message to the chat.
func (m ChatModel) AddUserMessage(content string) ChatModel {
	m.messages = append(m.messages, ChatMessage{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})
	m.updateContent()
	m.viewport.GotoBottom()
	m.userScrolled = false // Reset scroll state for new conversation turn
	return m
}

// AddAssistantMessage adds an assistant message to the chat.
func (m ChatModel) AddAssistantMessage(content string) ChatModel {
	return m.AddAssistantMessageWithToolCalls(content, nil)
}

// AddAssistantMessageWithToolCalls adds an assistant message with tool calls to the chat.
func (m ChatModel) AddAssistantMessageWithToolCalls(content string, toolCalls []ToolCallInfo) ChatModel {
	m.messages = append(m.messages, ChatMessage{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
		Timestamp: time.Now(),
	})
	m.updateContent()
	m.viewport.GotoBottom()
	return m
}

// AddSystemMessage adds a system message to the chat.
func (m ChatModel) AddSystemMessage(content string) ChatModel {
	m.messages = append(m.messages, ChatMessage{
		Role:      "system",
		Content:   content,
		Timestamp: time.Now(),
	})
	m.updateContent()
	m.viewport.GotoBottom()
	return m
}

// AddToolResult adds a tool result message to the chat.
func (m ChatModel) AddToolResult(toolCallID, name, result string) ChatModel {
	m.messages = append(m.messages, ChatMessage{
		Role:       "tool",
		Content:    result,
		ToolCallID: toolCallID,
		Name:       name,
		Timestamp:  time.Now(),
	})
	m.updateContent()
	// Only auto-scroll if user hasn't manually scrolled
	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
	return m
}

// AppendToLastAssistant appends content to the last assistant message.
func (m ChatModel) AppendToLastAssistant(content string) ChatModel {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "assistant" {
			m.messages[i].Content += content
			break
		}
	}
	m.updateContent()
	m.viewport.GotoBottom()
	return m
}

// SetLastAssistantContent sets the content of the last assistant message.
func (m ChatModel) SetLastAssistantContent(content string) ChatModel {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "assistant" {
			m.messages[i].Content = content
			break
		}
	}
	m.updateContent()
	// Only auto-scroll if user hasn't manually scrolled
	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
	return m
}

// AddFunctionCall adds a function call display to the chat.
func (m ChatModel) AddFunctionCall(call FunctionCall) ChatModel {
	m.functionCalls = append(m.functionCalls, call)
	m.updateContent()
	m.viewport.GotoBottom()
	return m
}

// UpdateFunctionResult updates the result of a function call.
func (m ChatModel) UpdateFunctionResult(name, result string) ChatModel {
	for i := len(m.functionCalls) - 1; i >= 0; i-- {
		if m.functionCalls[i].Name == name && m.functionCalls[i].Status == "executing" {
			m.functionCalls[i].Result = result
			m.functionCalls[i].Status = "completed"
			break
		}
	}
	m.updateContent()
	return m
}

// GetMessages returns all chat messages.
func (m ChatModel) GetMessages() []ChatMessage {
	return m.messages
}

// Clear clears all messages and function calls.
func (m ChatModel) Clear() ChatModel {
	m.messages = []ChatMessage{}
	m.functionCalls = []FunctionCall{}
	m.updateContent()
	return m
}

// ToggleSkillCalls toggles the visibility of skill call logs.
func (m ChatModel) ToggleSkillCalls() ChatModel {
	m.showSkillCalls = !m.showSkillCalls
	m.updateContent()
	return m
}

// updateContent rebuilds the viewport content from messages.
func (m *ChatModel) updateContent() {
	if !m.ready {
		return
	}

	var lines []string
	contentWidth := m.width - 4 // Account for padding and some margin

	// Render messages (skip tool results - only LLM needs to see them)
	for _, msg := range m.messages {
		// Don't render tool results in UI - they're for LLM only
		if msg.Role == "tool" {
			continue
		}
		lines = append(lines, m.renderMessage(msg, contentWidth))
		lines = append(lines, "") // Spacing between messages
	}

	// Render function calls (only if showSkillCalls is true)
	if m.showSkillCalls {
		for _, call := range m.functionCalls {
			lines = append(lines, m.renderFunctionCall(call, contentWidth))
		}
	}

	content := strings.Join(lines, "\n")
	m.viewport.SetContent(content)
}

// renderMessage renders a single chat message.
func (m ChatModel) renderMessage(msg ChatMessage, width int) string {
	// Format timestamp
	ts := msg.Timestamp.Format("15:04")
	timestamp := TimestampStyle.Render(ts)

	// Format role label
	var roleLabel string
	switch msg.Role {
	case "user":
		roleLabel = UserMessageStyle.Bold(true).Render("You")
	case "assistant":
		roleLabel = AssistantMessageStyle.Bold(true).Render("Celeste")
	case "system":
		roleLabel = SystemMessageStyle.Bold(true).Render("System")
	}

	// Header line
	header := fmt.Sprintf("%s %s", roleLabel, timestamp)

	// Detect pre-formatted content (stats dashboard, ASCII art, etc.)
	// These have box-drawing characters or block elements that shouldn't be wrapped
	isPreformatted := strings.Contains(msg.Content, "▓▒░") ||
		strings.Contains(msg.Content, "═══") ||
		strings.Contains(msg.Content, "█") ||
		strings.Contains(msg.Content, "```")

	// Wrap content to width (skip wrapping for pre-formatted content)
	contentStyle := MessageRoleStyle(msg.Role)
	var wrappedContent string
	if isPreformatted {
		wrappedContent = msg.Content // Don't wrap pre-formatted content
	} else {
		wrappedContent = wrapText(msg.Content, width-2)
	}
	styledContent := contentStyle.Render(wrappedContent)

	return lipgloss.JoinVertical(lipgloss.Left, header, styledContent)
}

// renderFunctionCall renders a function call display.
func (m ChatModel) renderFunctionCall(call FunctionCall, width int) string {
	// Status indicator
	var statusIndicator string
	switch call.Status {
	case "executing":
		statusIndicator = SkillExecutingStyle.Render("⏳")
	case "completed":
		statusIndicator = SkillCompletedStyle.Render("✓")
	case "error":
		statusIndicator = SkillErrorStyle.Render("✗")
	default:
		statusIndicator = SkillNameStyle.Render("●")
	}

	// Function name
	name := FunctionNameStyle.Render(call.Name)

	// Arguments (truncated)
	argsStr := formatArgs(call.Arguments)
	if len(argsStr) > 50 {
		argsStr = argsStr[:47] + "..."
	}
	args := FunctionArgsStyle.Render(argsStr)

	// Result (if any)
	var result string
	if call.Result != "" {
		resultStr := call.Result
		if len(resultStr) > 100 {
			resultStr = resultStr[:97] + "..."
		}
		result = FunctionResultStyle.Render("→ " + resultStr)
	}

	header := fmt.Sprintf("%s %s %s", statusIndicator, name, args)
	content := header
	if result != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, header, result)
	}

	return FunctionCallStyle.Width(width - 4).Render(content)
}

// wrapText wraps text to the specified width.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}

		words := strings.Fields(line)
		if len(words) == 0 {
			continue
		}

		currentLine := words[0]
		for _, word := range words[1:] {
			if len(currentLine)+1+len(word) <= width {
				currentLine += " " + word
			} else {
				result.WriteString(currentLine)
				result.WriteString("\n")
				currentLine = word
			}
		}
		result.WriteString(currentLine)
	}

	return result.String()
}

// formatArgs formats function call arguments.
func formatArgs(args map[string]any) string {
	if len(args) == 0 {
		return "()"
	}

	var parts []string
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
