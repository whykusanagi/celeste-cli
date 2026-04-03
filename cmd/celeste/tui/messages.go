// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains message types used for communication between components.
package tui

import (
	"context"
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
	Metadata   map[string]any // Optional metadata (e.g. image data from tool results)
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

// StreamStartMsg carries the context cancel function so the TUI can cancel
// an in-progress LLM request on Ctrl+C.
type StreamStartMsg struct {
	Cancel context.CancelFunc
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

// SkillCallRequest represents one tool call request in a batch.
type SkillCallRequest struct {
	Call       FunctionCall
	ToolCallID string // OpenAI tool call ID for sending result back
	ParseError string // Non-empty when arguments failed to parse
}

// SkillCallBatchMsg is sent when the LLM requests one or more skill/function calls.
type SkillCallBatchMsg struct {
	Calls            []SkillCallRequest
	AssistantContent string         // Assistant message content (may be empty if only tool calls)
	ToolCalls        []ToolCallInfo // Raw tool call payloads from assistant message
}

// SkillResultMsg is sent when a skill execution completes.
type SkillResultMsg struct {
	Name       string
	Result     string
	Err        error
	ToolCallID string         // OpenAI tool call ID for sending result back
	Metadata   map[string]any // Optional metadata (e.g. image base64 from read_file)
}

// AgentCommandResultMsg is sent when a TUI /agent command completes.
type AgentCommandResultMsg struct {
	Output string
	Err    error
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

// NSFWToggleMsg is sent when NSFW mode is toggled.
type NSFWToggleMsg struct {
	Enabled bool
}

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

// AgentProgressKind identifies the type of an AgentProgressMsg.
type AgentProgressKind int

const (
	AgentProgressTurnStart AgentProgressKind = iota // a new turn has started
	AgentProgressToolCall                           // agent called a tool
	AgentProgressStepDone                           // a plan step was marked done
	AgentProgressResponse                           // final assistant response text
	AgentProgressComplete                           // run finished successfully
	AgentProgressError                              // run failed
)

// AgentProgressMsg is sent incrementally during an agent run.
// Ch is a channel of further progress messages; nil on terminal kinds (Complete/Error).
type AgentProgressMsg struct {
	RunID    string
	Kind     AgentProgressKind
	Text     string
	Turn     int
	MaxTurns int
	// Per-turn stats — populated on ProgressResponse and ProgressComplete kinds.
	InputTokens  int
	OutputTokens int
	Duration     time.Duration
	Ch           <-chan AgentProgressMsg
}

// ReadNext returns a tea.Cmd that reads the next AgentProgressMsg from Ch.
// Returns nil when Ch is nil or closed — no command to schedule.
func (m AgentProgressMsg) ReadNext() tea.Cmd {
	if m.Ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-m.Ch
		if !ok {
			return nil
		}
		return msg
	}
}

// OrchestratorEventMsg wraps an orchestrator.OrchestratorEvent for delivery to the TUI.
// Defined here to avoid an import cycle (tui → orchestrator is fine; orchestrator must not → tui).
type OrchestratorEventMsg struct {
	Kind         int    // cast from orchestrator.EventKind
	Lane         string // cast from orchestrator.TaskLane
	Text         string
	Model        string // model name where role matters (primary/reviewer)
	Duration     time.Duration
	InputTokens  int
	OutputTokens int
	Response     string // full assistant response text for live code output panel
	FilePath     string
	Diff         string
	Score        float64
	Ch           <-chan OrchestratorEventMsg // nil on terminal events
}

// ReadNext returns a cmd to read the next OrchestratorEventMsg.
func (m OrchestratorEventMsg) ReadNext() tea.Cmd {
	if m.Ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-m.Ch
		if !ok {
			return nil
		}
		return msg
	}
}

// --- v1.7 TUI Enhancement Messages ---

// ToolProgressMsg reports real-time tool execution status.
type ToolProgressMsg struct {
	ToolCallID string
	ToolName   string
	State      string // "executing", "done", "failed", "aborted"
	Message    string // progress message
	Elapsed    time.Duration
}

// PermissionRequestMsg asks the user for permission to run a tool.
type PermissionRequestMsg struct {
	ToolCallID   string
	ToolName     string
	InputSummary string // short description of what the tool wants to do
	RiskLevel    string // "read", "write", "destructive"
	Response     chan PermissionResponse
}

// PermissionResponse is the user's answer to a permission request.
type PermissionResponse struct {
	Decision string // "allow_once", "always_allow", "deny", "always_deny"
	Pattern  string // rule pattern for "always" decisions
}

// ContextBudgetMsg updates the context budget display.
type ContextBudgetMsg struct {
	UsedTokens   int
	MaxTokens    int
	UsagePercent float64
	CompactCount int
	TurnCount    int
}

// MCPStatusMsg updates the MCP server status display.
type MCPStatusMsg struct {
	Servers []MCPServerInfo
}

// MCPServerInfo describes the status of a single MCP server.
type MCPServerInfo struct {
	Name      string
	Transport string
	Connected bool
	ToolCount int
}
