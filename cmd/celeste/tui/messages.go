// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains message types used for communication between components.
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ChatMessage represents a message in the conversation.
type ChatMessage struct {
	Role       string         // "user", "assistant", "system", "tool"
	Content    string         // Message content
	ToolCallID string         // For tool messages, the tool call ID
	Name       string         // For tool messages, the function name
	ToolCalls  []ToolCallInfo // For assistant messages, the tool calls that were made
	Timestamp  time.Time      // When the message was created
}

// ToolCallInfo represents a tool call in an assistant message.
type ToolCallInfo struct {
	ID        string
	Name      string
	Arguments string
}

// FunctionCall represents a tool/function call from the LLM.
type FunctionCall struct {
	Name      string         // Function name
	Arguments map[string]any // Arguments passed to the function
	Result    string         // Result of the function call
	Status    string         // "executing", "completed", "error"
	Timestamp time.Time      // When the call was initiated
}

// StreamChunk represents a piece of streamed response.
type StreamChunk struct {
	Content      string // Content delta
	IsFirst      bool   // Is this the first chunk?
	IsFinal      bool   // Is this the last chunk?
	FinishReason string // Reason for finishing (if final)
}

// --- Bubble Tea Messages ---

// StreamChunkMsg is sent when a new stream chunk arrives.
type StreamChunkMsg struct {
	Chunk StreamChunk
}

// TokenUsage holds token usage information from API response
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// StreamDoneMsg is sent when streaming is complete.
type StreamDoneMsg struct {
	FullContent  string
	FinishReason string
	Usage        *TokenUsage // Token usage from API (if available)
}

// StreamErrorMsg is sent when streaming encounters an error.
type StreamErrorMsg struct {
	Err error
}

// SkillCallMsg is sent when the LLM wants to call a skill/function.
type SkillCallMsg struct {
	Call             FunctionCall
	ToolCallID       string         // OpenAI tool call ID for sending result back
	AssistantContent string         // The assistant message content (may be empty if only tool calls)
	ToolCalls        []ToolCallInfo // All tool calls from the assistant message
}

// SkillResultMsg is sent when a skill execution completes.
type SkillResultMsg struct {
	Name       string
	Result     string
	Err        error
	ToolCallID string // OpenAI tool call ID for sending result back
}

// SendMessageMsg is sent when the user submits a message.
type SendMessageMsg struct {
	Content string
}

// TickMsg is sent for timer-based updates (animations, etc).
type TickMsg struct {
	Time time.Time
}

// SimulateTypingMsg is sent to simulate typing effect.
type SimulateTypingMsg struct {
	Content     string // Full content to simulate typing
	CharsToShow int    // How many characters to show now
}

// ErrorMsg is sent when an error occurs.
type ErrorMsg struct {
	Err error
}

// StatusMsg is sent to update the status bar.
type StatusMsg struct {
	Text string
}

// NSFWToggleMsg is sent when NSFW mode is toggled.
type NSFWToggleMsg struct {
	Enabled bool
}

// SessionLoadedMsg is sent when a session is loaded.
type SessionLoadedMsg struct {
	Messages []ChatMessage
}

// ClearChatMsg is sent to clear the chat history.
type ClearChatMsg struct{}

// ExitMsg is sent to exit the application.
type ExitMsg struct{}

// GenerateMediaMsg is sent to generate media (image/video/etc) via Venice.ai.
type GenerateMediaMsg struct {
	MediaType  string
	Prompt     string
	Params     map[string]interface{}
	ImageModel string // Override image model (if set)
}

// MediaResultMsg is sent when media generation completes.
type MediaResultMsg struct {
	Success   bool
	URL       string
	Path      string
	Error     string
	MediaType string
}

// ShowSelectorMsg triggers the interactive selector.
type ShowSelectorMsg struct {
	Title string
	Items []SelectorItem
}

// --- Commands ---

// Tick returns a command that sends a tick message after a delay.
func Tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return TickMsg{Time: t}
	})
}

// SendMessage returns a command that sends a message to the LLM.
func SendMessage(content string) tea.Cmd {
	return func() tea.Msg {
		return SendMessageMsg{Content: content}
	}
}

// Error returns a command that sends an error message.
func Error(err error) tea.Cmd {
	return func() tea.Msg {
		return ErrorMsg{Err: err}
	}
}

// ClearChat returns a command that clears the chat.
func ClearChat() tea.Cmd {
	return func() tea.Msg {
		return ClearChatMsg{}
	}
}

// Exit returns a command that exits the application.
func Exit() tea.Cmd {
	return func() tea.Msg {
		return ExitMsg{}
	}
}
