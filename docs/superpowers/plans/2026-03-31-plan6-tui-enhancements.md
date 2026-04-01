# Plan 6: TUI Enhancements

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface the new tool execution, context management, permissions, and MCP subsystems in the Bubble Tea TUI with 4 new components and targeted modifications to existing ones.

**Architecture:** 4 new Bubble Tea components added to the existing `AppModel` hierarchy. Each component follows the standard Model/Update/View pattern established by `ChatModel`, `InputModel`, `SelectorModel`, etc. New message types added to `messages.go` for cross-component communication. All styling uses the existing corrupted-theme Lip Gloss palette from `styles.go`.

**Tech Stack:** Go 1.26, `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `github.com/charmbracelet/bubbles`

**Prerequisite Plans:** Plans 1-5 (Unified Tool Layer, Streaming Tool Executor, Context Budget, Permissions, MCP)

---

## Codebase Context

**Current TUI architecture (all files under `cmd/celeste/tui/`):**

1. **`app.go`** (2038 lines) -- `AppModel` is the root Bubble Tea model. Contains sub-components (`header`, `chat`, `input`, `skills`, `status`), application state flags, pending tool call tracking, and the main `Update()/View()` dispatch. `View()` composes: header -> chat -> input -> skills -> status bar. Split panel mode used for orchestrator/agent views.

2. **`chat.go`** (417 lines) -- `ChatModel` with `viewport.Model`, message list, function call rendering. Methods: `AddUserMessage()`, `AddAssistantMessage()`, `AddFunctionCall()`, `UpdateFunctionResult()`, `SetSize()`.

3. **`input.go`** (252 lines) -- `InputModel` with `textinput.Model`, command history, typeahead suggestions. Known commands list in `knownCommands` slice.

4. **`streaming.go`** (451 lines) -- `SimulatedTyping` with corruption/glitch effects. Character-by-character reveal with Japanese/English corruption phrases.

5. **`messages.go`** (244 lines) -- All Bubble Tea message types: `ChatMessage`, `StreamChunk`, `StreamChunkMsg`, `StreamDoneMsg`, `StreamErrorMsg`, `SkillCallMsg`, `SkillCallBatchMsg`, `SkillResultMsg`, `AgentProgressMsg`, `OrchestratorEventMsg`, `TickMsg`, etc. Channel-based `ReadNext()` pattern for async progress.

6. **`styles.go`** (247 lines) -- Lip Gloss styles with corrupted-theme palette. Colors: `ColorAccent` (#d94f90), `ColorPurple` (#8b5cf6), `ColorCyan` (#00d4ff), `ColorSuccess` (#22c55e), `ColorError` (#ef4444), `ColorWarning` (#eab308). Component styles: `ChatPanelStyle`, `InputPanelStyle`, `SkillsPanelStyle`, `FunctionCallStyle`, etc.

7. **`skills_panel.go`** -- `SkillsModel` for runtime skill status display.
8. **`collections.go`** -- `CollectionsModel` for xAI Collections (list/toggle pattern with viewport).
9. **`menu.go`** -- `MenuModel` for command menu.
10. **`selector.go`** -- `SelectorModel` with arrow-key navigation, scroll offset, confirm/cancel.

**Key patterns established in the codebase:**
- Sub-components use value receivers and return modified copies (e.g., `func (m ChatModel) AddUserMessage(...) ChatModel`)
- `SetSize(width, height)` method on each component for resize handling
- `Init() tea.Cmd`, `Update(msg) (Model, tea.Cmd)`, `View() string` on all components
- View modes in AppModel: `"chat"`, `"collections"`, `"menu"`, `"skills"` controlled by `viewMode` string
- Async operations use channel-based message delivery with `ReadNext()` commands
- Tool call tracking via `pendingToolCalls` slice and `toolBatchActive` bool

**What Plans 1-5 provide (referenced types):**

- **Plan 1:** `tools.Tool` interface with `Execute(ctx, input) (ToolResult, error)`, `tools.Registry` with `Get(name)`, `ListByMode(mode)`, `tools.ProgressEvent{Type, Message, Percent}`
- **Plan 2:** `StreamingToolExecutor` with progress channel, `StreamEvent` types in LLM backends for real-time tool output
- **Plan 3:** `context.TokenBudget{Used, Limit, Turns, Compactions}` with `Usage()` snapshot, compaction triggers
- **Plan 4:** `permissions.Checker` with `Check(tool, input) CheckResult` returning `Allow`/`Deny`/`Ask`, `permissions.SaveConfig()` for persisting "Always" rules
- **Plan 5:** MCP `Manager` with `ListServers() []ServerStatus`, `ServerStatus{Name, Transport, Connected, ToolCount, LastError, Tools []ToolInfo}`

---

## File Structure

```
cmd/celeste/tui/                          # MODIFIED package
├── messages.go                           # MODIFIED - add 5 new message types
├── styles.go                             # MODIFIED - add styles for new components
├── tool_progress.go                      # NEW - Tool progress indicators
├── tool_progress_test.go                 # NEW - Tests
├── context_bar.go                        # NEW - Context budget status bar
├── context_bar_test.go                   # NEW - Tests
├── permissions.go                        # NEW - Permission prompt dialog
├── permissions_test.go                   # NEW - Tests
├── mcp_panel.go                          # NEW - MCP server/tool browser
├── mcp_panel_test.go                     # NEW - Tests
├── app.go                                # MODIFIED - wire new components
├── input.go                              # MODIFIED - add "mcp" to knownCommands
```

---

## Task 1: New Message Types

Add 5 new message types to `tui/messages.go` for cross-component communication.

### Steps

- [ ] **1.1** Add `ToolProgressMsg` to `messages.go`
- [ ] **1.2** Add `PermissionRequestMsg` and `PermissionResponseMsg` to `messages.go`
- [ ] **1.3** Add `ContextBudgetMsg` to `messages.go`
- [ ] **1.4** Add `MCPStatusMsg` to `messages.go`
- [ ] **1.5** Write tests in `messages_test.go`

### Complete Code

**File: `cmd/celeste/tui/messages.go`** -- append after existing types (before closing of file):

```go
// --- Plan 6: New message types for TUI enhancements ---

// ToolProgressState represents the execution state of a tool.
type ToolProgressState int

const (
	ToolProgressExecuting ToolProgressState = iota
	ToolProgressDone
	ToolProgressFailed
)

// String returns a human-readable label for the state.
func (s ToolProgressState) String() string {
	switch s {
	case ToolProgressExecuting:
		return "executing"
	case ToolProgressDone:
		return "done"
	case ToolProgressFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// ToolProgressMsg is sent when a tool's execution status changes.
// Emitted by the streaming tool executor (Plan 2) and consumed by ToolProgressModel.
type ToolProgressMsg struct {
	ToolCallID string            // Correlates with pending tool call
	ToolName   string            // Human-readable tool name
	State      ToolProgressState // Current execution state
	Message    string            // Progress message (e.g., "Reading file...", "3/10 results")
	Percent    float64           // 0.0-1.0 progress if known, -1 for indeterminate
	Elapsed    time.Duration     // Time since execution started
}

// PermissionDecision represents the user's response to a permission prompt.
type PermissionDecision int

const (
	PermissionAllowOnce   PermissionDecision = iota // Allow this single invocation
	PermissionAlwaysAllow                           // Allow this tool+pattern forever
	PermissionDenyOnce                              // Deny this single invocation
	PermissionAlwaysDeny                            // Deny this tool+pattern forever
)

// PermissionRiskLevel classifies the risk of a tool invocation.
type PermissionRiskLevel int

const (
	PermissionRiskLow    PermissionRiskLevel = iota // Read-only, no side effects
	PermissionRiskMedium                            // File writes, network calls
	PermissionRiskHigh                              // Shell execution, destructive ops
)

// String returns a human-readable label.
func (r PermissionRiskLevel) String() string {
	switch r {
	case PermissionRiskLow:
		return "low"
	case PermissionRiskMedium:
		return "medium"
	case PermissionRiskHigh:
		return "high"
	default:
		return "unknown"
	}
}

// PermissionRequestMsg is sent when the permissions checker (Plan 4) returns Ask.
// The TUI renders an inline dialog and the user responds with a key press.
// ResponseCh is a buffered channel (cap 1) that the TUI writes the decision to.
type PermissionRequestMsg struct {
	ToolCallID   string                // Which pending tool call is blocked
	ToolName     string                // Human-readable tool name
	InputSummary string                // One-line summary of tool input (truncated)
	RiskLevel    PermissionRiskLevel   // Risk classification
	ResponseCh   chan PermissionDecision // Buffered channel for the user's response
}

// PermissionResponseMsg is sent after the user makes a permission decision.
// Consumed by AppModel to resume or abort the blocked tool call.
type PermissionResponseMsg struct {
	ToolCallID string             // Which tool call this responds to
	Decision   PermissionDecision // The user's choice
}

// ContextBudgetSnapshot holds a point-in-time view of token budget usage.
type ContextBudgetSnapshot struct {
	Used        int     // Tokens consumed so far
	Limit       int     // Maximum token budget
	Percent     float64 // Used/Limit as 0.0-1.0
	Turns       int     // Number of conversation turns
	Compactions int     // Number of compactions performed
}

// ContextBudgetMsg is sent when token budget usage changes.
// Emitted after each LLM round-trip and after compaction events.
type ContextBudgetMsg struct {
	Budget ContextBudgetSnapshot
}

// MCPServerInfo holds status for a single MCP server.
type MCPServerInfo struct {
	Name      string // Server display name
	Transport string // "stdio", "sse", "streamable-http"
	Connected bool   // Whether the server is currently reachable
	ToolCount int    // Number of tools provided by this server
	LastError string // Last error message (empty if healthy)
	Tools     []MCPToolInfo // Tools provided by this server
}

// MCPToolInfo holds metadata for a single MCP tool.
type MCPToolInfo struct {
	Name        string // Tool name
	Description string // Tool description
}

// MCPStatusMsg is sent with the current state of all MCP servers.
// Emitted by the MCP manager (Plan 5) and consumed by MCPPanelModel.
type MCPStatusMsg struct {
	Servers []MCPServerInfo
}
```

**File: `cmd/celeste/tui/messages_test.go`** (NEW):

```go
package tui

import (
	"testing"
	"time"
)

func TestToolProgressState_String(t *testing.T) {
	tests := []struct {
		state ToolProgressState
		want  string
	}{
		{ToolProgressExecuting, "executing"},
		{ToolProgressDone, "done"},
		{ToolProgressFailed, "failed"},
		{ToolProgressState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("ToolProgressState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestPermissionRiskLevel_String(t *testing.T) {
	tests := []struct {
		level PermissionRiskLevel
		want  string
	}{
		{PermissionRiskLow, "low"},
		{PermissionRiskMedium, "medium"},
		{PermissionRiskHigh, "high"},
		{PermissionRiskLevel(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("PermissionRiskLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestPermissionRequestMsg_ResponseChannel(t *testing.T) {
	ch := make(chan PermissionDecision, 1)
	msg := PermissionRequestMsg{
		ToolCallID:   "call_123",
		ToolName:     "bash",
		InputSummary: "rm -rf /tmp/test",
		RiskLevel:    PermissionRiskHigh,
		ResponseCh:   ch,
	}

	// Simulate user responding
	msg.ResponseCh <- PermissionAlwaysDeny

	got := <-ch
	if got != PermissionAlwaysDeny {
		t.Errorf("expected PermissionAlwaysDeny, got %d", got)
	}
}

func TestToolProgressMsg_Fields(t *testing.T) {
	msg := ToolProgressMsg{
		ToolCallID: "call_456",
		ToolName:   "read_file",
		State:      ToolProgressExecuting,
		Message:    "Reading /etc/hosts...",
		Percent:    0.5,
		Elapsed:    2 * time.Second,
	}

	if msg.State != ToolProgressExecuting {
		t.Errorf("expected ToolProgressExecuting, got %v", msg.State)
	}
	if msg.Percent != 0.5 {
		t.Errorf("expected 0.5, got %f", msg.Percent)
	}
}

func TestContextBudgetSnapshot_Fields(t *testing.T) {
	snap := ContextBudgetSnapshot{
		Used:        12400,
		Limit:       128000,
		Percent:     0.096875,
		Turns:       7,
		Compactions: 0,
	}

	if snap.Used != 12400 {
		t.Errorf("expected 12400, got %d", snap.Used)
	}
	if snap.Turns != 7 {
		t.Errorf("expected 7 turns, got %d", snap.Turns)
	}
}

func TestMCPStatusMsg_ServerList(t *testing.T) {
	msg := MCPStatusMsg{
		Servers: []MCPServerInfo{
			{
				Name:      "filesystem",
				Transport: "stdio",
				Connected: true,
				ToolCount: 5,
				Tools: []MCPToolInfo{
					{Name: "read_file", Description: "Read a file"},
				},
			},
			{
				Name:      "database",
				Transport: "sse",
				Connected: false,
				ToolCount: 0,
				LastError: "connection refused",
			},
		},
	}

	if len(msg.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(msg.Servers))
	}
	if !msg.Servers[0].Connected {
		t.Error("expected first server to be connected")
	}
	if msg.Servers[1].Connected {
		t.Error("expected second server to be disconnected")
	}
	if msg.Servers[1].LastError != "connection refused" {
		t.Errorf("unexpected error: %s", msg.Servers[1].LastError)
	}
}
```

---

## Task 2: Tool Progress Component

Real-time tool execution status displayed inline in the chat viewport. Shows spinner for executing, checkmark for done, X for failed. Concurrent tools stacked. Completed tools collapse to a single line after 2 seconds.

### Steps

- [ ] **2.1** Create `tui/tool_progress.go` with `ToolProgressModel` struct
- [ ] **2.2** Implement `Update()` to handle `ToolProgressMsg` and `TickMsg` for elapsed timer
- [ ] **2.3** Implement `View()` with spinner/checkmark/X, progress message, elapsed time
- [ ] **2.4** Implement collapse logic: completed tools shrink to one line after 2s
- [ ] **2.5** Write tests in `tui/tool_progress_test.go`

### Complete Code

**File: `cmd/celeste/tui/tool_progress.go`** (NEW):

```go
// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the tool progress indicator component.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// collapseDelay is how long after completion before a tool collapses to one line.
const collapseDelay = 2 * time.Second

// spinnerFrames are the animation frames for the executing spinner.
// Uses a corrupted-aesthetic spinner rather than the typical Bubble Tea spinner.
var spinnerFrames = []string{"[:::]", "[::.:]", "[:.::.]", "[:..:]", "[:.::.]", "[::.:]"}

// toolExecution tracks one active or recently-completed tool execution.
type toolExecution struct {
	toolCallID string
	toolName   string
	state      ToolProgressState
	message    string            // Latest progress message
	percent    float64           // -1 for indeterminate
	startedAt  time.Time
	finishedAt time.Time         // Zero value if still executing
	collapsed  bool              // True once collapseDelay has elapsed after completion
}

// elapsed returns the duration since execution started.
func (e toolExecution) elapsed() time.Duration {
	if e.finishedAt.IsZero() {
		return time.Since(e.startedAt)
	}
	return e.finishedAt.Sub(e.startedAt)
}

// shouldCollapse returns true if enough time has passed since completion.
func (e toolExecution) shouldCollapse() bool {
	if e.state == ToolProgressExecuting {
		return false
	}
	return !e.finishedAt.IsZero() && time.Since(e.finishedAt) >= collapseDelay
}

// ToolProgressModel displays real-time tool execution status.
// It tracks multiple concurrent tool executions and renders them stacked.
type ToolProgressModel struct {
	executions []toolExecution
	width      int
	animFrame  int // Current spinner frame index
}

// NewToolProgressModel creates a new tool progress model.
func NewToolProgressModel() ToolProgressModel {
	return ToolProgressModel{}
}

// SetWidth sets the rendering width.
func (m ToolProgressModel) SetWidth(width int) ToolProgressModel {
	m.width = width
	return m
}

// HasActive returns true if any tool is currently executing.
func (m ToolProgressModel) HasActive() bool {
	for _, e := range m.executions {
		if e.state == ToolProgressExecuting {
			return true
		}
	}
	return false
}

// IsEmpty returns true if there are no executions to display.
func (m ToolProgressModel) IsEmpty() bool {
	// Empty if no executions, or all are collapsed
	for _, e := range m.executions {
		if !e.collapsed {
			return false
		}
	}
	return true
}

// Clear removes all collapsed executions. Called when a new user message starts.
func (m ToolProgressModel) Clear() ToolProgressModel {
	m.executions = nil
	return m
}

// Update handles messages for the tool progress component.
func (m ToolProgressModel) Update(msg tea.Msg) (ToolProgressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ToolProgressMsg:
		return m.handleProgress(msg), nil

	case TickMsg:
		// Advance spinner frame
		m.animFrame = (m.animFrame + 1) % len(spinnerFrames)

		// Check for newly collapsible executions
		for i := range m.executions {
			if !m.executions[i].collapsed && m.executions[i].shouldCollapse() {
				m.executions[i].collapsed = true
			}
		}

		// Prune fully collapsed entries that finished more than 10s ago
		pruned := m.executions[:0]
		for _, e := range m.executions {
			if e.collapsed && !e.finishedAt.IsZero() && time.Since(e.finishedAt) > 10*time.Second {
				continue // Drop it
			}
			pruned = append(pruned, e)
		}
		m.executions = pruned

		return m, nil
	}

	return m, nil
}

// handleProgress updates or creates a tool execution entry.
func (m ToolProgressModel) handleProgress(msg ToolProgressMsg) ToolProgressModel {
	// Find existing execution by tool call ID
	for i, e := range m.executions {
		if e.toolCallID == msg.ToolCallID {
			m.executions[i].state = msg.State
			m.executions[i].message = msg.Message
			m.executions[i].percent = msg.Percent
			if msg.State != ToolProgressExecuting && m.executions[i].finishedAt.IsZero() {
				m.executions[i].finishedAt = time.Now()
			}
			return m
		}
	}

	// New execution
	exec := toolExecution{
		toolCallID: msg.ToolCallID,
		toolName:   msg.ToolName,
		state:      msg.State,
		message:    msg.Message,
		percent:    msg.Percent,
		startedAt:  time.Now(),
	}
	if msg.State != ToolProgressExecuting {
		exec.finishedAt = time.Now()
	}
	m.executions = append(m.executions, exec)
	return m
}

// View renders all active and recently-completed tool executions.
func (m ToolProgressModel) View() string {
	if m.IsEmpty() {
		return ""
	}

	var lines []string
	for _, e := range m.executions {
		if e.collapsed {
			// Collapsed: single muted line
			line := m.renderCollapsed(e)
			if line != "" {
				lines = append(lines, line)
			}
			continue
		}
		lines = append(lines, m.renderExecution(e))
	}

	if len(lines) == 0 {
		return ""
	}

	content := strings.Join(lines, "\n")

	// Wrap in a styled box
	return ToolProgressBoxStyle.
		Width(m.width - 4).
		Render(content)
}

// renderExecution renders a single tool execution (non-collapsed).
func (m ToolProgressModel) renderExecution(e toolExecution) string {
	// Status icon
	var icon string
	var iconStyle lipgloss.Style
	switch e.state {
	case ToolProgressExecuting:
		icon = spinnerFrames[m.animFrame]
		iconStyle = ToolProgressSpinnerStyle
	case ToolProgressDone:
		icon = "[ok]"
		iconStyle = ToolProgressDoneStyle
	case ToolProgressFailed:
		icon = "[!!]"
		iconStyle = ToolProgressFailedStyle
	}

	styledIcon := iconStyle.Render(icon)

	// Tool name
	name := ToolProgressNameStyle.Render(e.toolName)

	// Elapsed time
	elapsed := formatElapsed(e.elapsed())
	elapsedStr := ToolProgressElapsedStyle.Render(elapsed)

	// Header line: icon name elapsed
	header := fmt.Sprintf("%s %s %s", styledIcon, name, elapsedStr)

	// Progress message (if any)
	if e.message != "" {
		msg := ToolProgressMessageStyle.Render("  " + e.message)

		// Progress bar (if determinate)
		if e.percent >= 0 && e.percent <= 1.0 {
			bar := renderProgressBar(e.percent, m.width-8)
			return lipgloss.JoinVertical(lipgloss.Left, header, msg, "  "+bar)
		}

		return lipgloss.JoinVertical(lipgloss.Left, header, msg)
	}

	return header
}

// renderCollapsed renders a collapsed (completed) tool as a single line.
func (m ToolProgressModel) renderCollapsed(e toolExecution) string {
	var icon string
	var iconStyle lipgloss.Style
	switch e.state {
	case ToolProgressDone:
		icon = "[ok]"
		iconStyle = ToolProgressDoneStyle
	case ToolProgressFailed:
		icon = "[!!]"
		iconStyle = ToolProgressFailedStyle
	default:
		return ""
	}

	elapsed := formatElapsed(e.elapsed())
	return TextMutedStyle.Render(
		fmt.Sprintf("%s %s %s", iconStyle.Render(icon), e.toolName, elapsed),
	)
}

// renderProgressBar draws a simple bar: [=======>    ] 73%
func renderProgressBar(percent float64, width int) string {
	if width < 12 {
		return fmt.Sprintf("%.0f%%", percent*100)
	}

	barWidth := width - 8 // Room for brackets, space, percentage
	if barWidth < 5 {
		barWidth = 5
	}

	filled := int(float64(barWidth) * percent)
	if filled > barWidth {
		filled = barWidth
	}

	bar := strings.Repeat("=", filled)
	if filled < barWidth {
		bar += ">"
		bar += strings.Repeat(" ", barWidth-filled-1)
	}

	return fmt.Sprintf(
		"%s%s%s %s",
		ToolProgressBarBracketStyle.Render("["),
		ToolProgressBarFillStyle.Render(bar),
		ToolProgressBarBracketStyle.Render("]"),
		ToolProgressElapsedStyle.Render(fmt.Sprintf("%.0f%%", percent*100)),
	)
}

// formatElapsed formats a duration as a compact string.
func formatElapsed(d time.Duration) string {
	if d < time.Second {
		ms := d.Milliseconds()
		return fmt.Sprintf("%dms", ms)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}
```

**File: `cmd/celeste/tui/tool_progress_test.go`** (NEW):

```go
package tui

import (
	"strings"
	"testing"
	"time"
)

func TestToolProgressModel_HandleProgress_NewExecution(t *testing.T) {
	m := NewToolProgressModel()
	m = m.SetWidth(80)

	msg := ToolProgressMsg{
		ToolCallID: "call_1",
		ToolName:   "read_file",
		State:      ToolProgressExecuting,
		Message:    "Reading /etc/hosts...",
		Percent:    -1,
		Elapsed:    0,
	}

	m = m.handleProgress(msg)

	if len(m.executions) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(m.executions))
	}
	if m.executions[0].toolName != "read_file" {
		t.Errorf("expected read_file, got %s", m.executions[0].toolName)
	}
	if m.executions[0].state != ToolProgressExecuting {
		t.Errorf("expected executing state")
	}
}

func TestToolProgressModel_HandleProgress_UpdateExisting(t *testing.T) {
	m := NewToolProgressModel()

	// Start
	m = m.handleProgress(ToolProgressMsg{
		ToolCallID: "call_1",
		ToolName:   "bash",
		State:      ToolProgressExecuting,
		Message:    "Running...",
	})

	// Complete
	m = m.handleProgress(ToolProgressMsg{
		ToolCallID: "call_1",
		ToolName:   "bash",
		State:      ToolProgressDone,
		Message:    "Exit code 0",
	})

	if len(m.executions) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(m.executions))
	}
	if m.executions[0].state != ToolProgressDone {
		t.Errorf("expected done state, got %v", m.executions[0].state)
	}
	if m.executions[0].finishedAt.IsZero() {
		t.Error("expected finishedAt to be set")
	}
}

func TestToolProgressModel_ConcurrentExecutions(t *testing.T) {
	m := NewToolProgressModel()

	m = m.handleProgress(ToolProgressMsg{ToolCallID: "call_1", ToolName: "read_file", State: ToolProgressExecuting})
	m = m.handleProgress(ToolProgressMsg{ToolCallID: "call_2", ToolName: "bash", State: ToolProgressExecuting})
	m = m.handleProgress(ToolProgressMsg{ToolCallID: "call_3", ToolName: "search", State: ToolProgressExecuting})

	if len(m.executions) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(m.executions))
	}
	if !m.HasActive() {
		t.Error("expected HasActive to be true")
	}
}

func TestToolProgressModel_HasActive(t *testing.T) {
	m := NewToolProgressModel()

	if m.HasActive() {
		t.Error("empty model should not have active")
	}

	m = m.handleProgress(ToolProgressMsg{ToolCallID: "call_1", ToolName: "bash", State: ToolProgressDone})

	if m.HasActive() {
		t.Error("should not have active when all done")
	}
}

func TestToolProgressModel_Clear(t *testing.T) {
	m := NewToolProgressModel()
	m = m.handleProgress(ToolProgressMsg{ToolCallID: "call_1", ToolName: "bash", State: ToolProgressDone})
	m = m.Clear()

	if len(m.executions) != 0 {
		t.Errorf("expected 0 executions after clear, got %d", len(m.executions))
	}
}

func TestToolProgressModel_View_Executing(t *testing.T) {
	m := NewToolProgressModel()
	m = m.SetWidth(80)
	m = m.handleProgress(ToolProgressMsg{
		ToolCallID: "call_1",
		ToolName:   "read_file",
		State:      ToolProgressExecuting,
		Message:    "Reading...",
	})

	view := m.View()
	if view == "" {
		t.Error("expected non-empty view for executing tool")
	}
	if !strings.Contains(view, "read_file") {
		t.Error("expected view to contain tool name")
	}
	if !strings.Contains(view, "Reading...") {
		t.Error("expected view to contain progress message")
	}
}

func TestToolProgressModel_View_WithProgressBar(t *testing.T) {
	m := NewToolProgressModel()
	m = m.SetWidth(80)
	m = m.handleProgress(ToolProgressMsg{
		ToolCallID: "call_1",
		ToolName:   "search",
		State:      ToolProgressExecuting,
		Message:    "Searching files...",
		Percent:    0.5,
	})

	view := m.View()
	if !strings.Contains(view, "50%") {
		t.Error("expected view to contain percentage")
	}
}

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{1500 * time.Millisecond, "1.5s"},
		{30 * time.Second, "30.0s"},
		{90 * time.Second, "1m30s"},
		{5*time.Minute + 3*time.Second, "5m03s"},
	}
	for _, tt := range tests {
		got := formatElapsed(tt.d)
		if got != tt.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestRenderProgressBar(t *testing.T) {
	bar := renderProgressBar(0.75, 40)
	if !strings.Contains(bar, "75%") {
		t.Errorf("expected bar to contain 75%%, got %q", bar)
	}
	if !strings.Contains(bar, "[") || !strings.Contains(bar, "]") {
		t.Errorf("expected bar to contain brackets, got %q", bar)
	}
}

func TestToolExecution_ShouldCollapse(t *testing.T) {
	// Executing: should not collapse
	e := toolExecution{state: ToolProgressExecuting}
	if e.shouldCollapse() {
		t.Error("executing tool should not collapse")
	}

	// Just finished: should not collapse yet
	e = toolExecution{state: ToolProgressDone, finishedAt: time.Now()}
	if e.shouldCollapse() {
		t.Error("just-finished tool should not collapse immediately")
	}

	// Finished long ago: should collapse
	e = toolExecution{state: ToolProgressDone, finishedAt: time.Now().Add(-5 * time.Second)}
	if !e.shouldCollapse() {
		t.Error("tool finished 5s ago should collapse")
	}
}
```

---

## Task 3: Context Budget Bar

A thin status line rendered between the chat viewport and the input panel showing token usage, compaction count, and turn count.

### Steps

- [ ] **3.1** Create `tui/context_bar.go` with `ContextBarModel` struct
- [ ] **3.2** Implement `Update()` to handle `ContextBudgetMsg`
- [ ] **3.3** Implement `View()` with progress bar, percentage, color coding, compaction/turn counts
- [ ] **3.4** Implement narrow terminal collapse (<80 cols)
- [ ] **3.5** Write tests in `tui/context_bar_test.go`

### Complete Code

**File: `cmd/celeste/tui/context_bar.go`** (NEW):

```go
// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the context budget status bar component.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ContextBarModel displays token budget usage as a thin status line.
// Rendered between the chat viewport and the input panel.
type ContextBarModel struct {
	budget ContextBudgetSnapshot
	width  int
	active bool // True once we have received at least one budget update
}

// NewContextBarModel creates a new context bar model.
func NewContextBarModel() ContextBarModel {
	return ContextBarModel{}
}

// SetWidth sets the rendering width.
func (m ContextBarModel) SetWidth(width int) ContextBarModel {
	m.width = width
	return m
}

// Update handles messages for the context bar.
func (m ContextBarModel) Update(msg tea.Msg) (ContextBarModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ContextBudgetMsg:
		m.budget = msg.Budget
		m.active = true
	}
	return m, nil
}

// View renders the context budget bar.
// Format: diamond tokens: 12.4k / 128k [progress bar] 9% | compact: 0 | turn: 7
// Returns empty string if no budget data has been received yet.
func (m ContextBarModel) View() string {
	if !m.active {
		return ""
	}

	// Narrow terminal: minimal display
	if m.width < 80 {
		return m.viewNarrow()
	}

	return m.viewFull()
}

// viewFull renders the full-width context bar.
func (m ContextBarModel) viewFull() string {
	b := m.budget

	// Pick color based on usage percentage
	barColor := m.budgetColor()

	// Format token counts compactly
	usedStr := formatTokenCount(b.Used)
	limitStr := formatTokenCount(b.Limit)
	percentStr := fmt.Sprintf("%.0f%%", b.Percent*100)

	// Build the bar segments
	diamond := lipgloss.NewStyle().Foreground(barColor).Render("◆")
	tokenLabel := ContextBarLabelStyle.Render("tokens:")
	tokenValue := lipgloss.NewStyle().Foreground(barColor).Render(
		fmt.Sprintf("%s / %s", usedStr, limitStr),
	)

	// Mini progress bar (10 chars wide)
	progressBar := m.renderMiniBar(barColor)

	percentValue := lipgloss.NewStyle().Foreground(barColor).Bold(true).Render(percentStr)

	separator := ContextBarSepStyle.Render("|")

	compactLabel := ContextBarLabelStyle.Render("compact:")
	compactValue := ContextBarValueStyle.Render(fmt.Sprintf("%d", b.Compactions))

	turnLabel := ContextBarLabelStyle.Render("turn:")
	turnValue := ContextBarValueStyle.Render(fmt.Sprintf("%d", b.Turns))

	line := fmt.Sprintf(
		"%s %s %s %s %s %s %s %s %s %s %s",
		diamond, tokenLabel, tokenValue,
		progressBar, percentValue,
		separator, compactLabel, compactValue,
		separator, turnLabel, turnValue,
	)

	return ContextBarStyle.Width(m.width).Render(line)
}

// viewNarrow renders a minimal context bar for narrow terminals.
func (m ContextBarModel) viewNarrow() string {
	b := m.budget
	barColor := m.budgetColor()

	usedStr := formatTokenCount(b.Used)
	percentStr := fmt.Sprintf("%.0f%%", b.Percent*100)

	diamond := lipgloss.NewStyle().Foreground(barColor).Render("◆")
	info := lipgloss.NewStyle().Foreground(barColor).Render(
		fmt.Sprintf("%s %s t:%d", usedStr, percentStr, b.Turns),
	)

	return ContextBarStyle.Width(m.width).Render(fmt.Sprintf("%s %s", diamond, info))
}

// renderMiniBar renders a 10-character progress bar using block elements.
func (m ContextBarModel) renderMiniBar(color lipgloss.Color) string {
	const barWidth = 10
	filled := int(m.budget.Percent * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}

	filledStr := strings.Repeat("▓", filled)
	emptyStr := strings.Repeat("░", barWidth-filled)

	return lipgloss.NewStyle().Foreground(color).Render(filledStr) +
		ContextBarDimStyle.Render(emptyStr)
}

// budgetColor returns the appropriate color based on usage percentage.
func (m ContextBarModel) budgetColor() lipgloss.Color {
	switch {
	case m.budget.Percent >= 0.80:
		return lipgloss.Color(ColorError)    // Red: >80%
	case m.budget.Percent >= 0.50:
		return lipgloss.Color(ColorWarning)  // Yellow: 50-80%
	default:
		return lipgloss.Color(ColorSuccess)  // Green: <50%
	}
}

// formatTokenCount formats a token count as a compact string.
// Examples: 500 -> "500", 1200 -> "1.2k", 128000 -> "128k", 1500000 -> "1.5M"
func formatTokenCount(count int) string {
	switch {
	case count >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	case count >= 1_000:
		v := float64(count) / 1_000
		if v == float64(int(v)) {
			return fmt.Sprintf("%dk", int(v))
		}
		return fmt.Sprintf("%.1fk", v)
	default:
		return fmt.Sprintf("%d", count)
	}
}
```

**File: `cmd/celeste/tui/context_bar_test.go`** (NEW):

```go
package tui

import (
	"strings"
	"testing"
)

func TestContextBarModel_InactiveByDefault(t *testing.T) {
	m := NewContextBarModel()
	m = m.SetWidth(100)

	if m.View() != "" {
		t.Error("expected empty view before first budget update")
	}
}

func TestContextBarModel_Update_ActivatesOnBudget(t *testing.T) {
	m := NewContextBarModel()
	m = m.SetWidth(100)

	m, _ = m.Update(ContextBudgetMsg{
		Budget: ContextBudgetSnapshot{
			Used:    12400,
			Limit:   128000,
			Percent: 0.096875,
			Turns:   7,
		},
	})

	if !m.active {
		t.Error("expected active after budget message")
	}
	view := m.View()
	if view == "" {
		t.Error("expected non-empty view after budget update")
	}
}

func TestContextBarModel_View_ContainsTokenInfo(t *testing.T) {
	m := NewContextBarModel()
	m = m.SetWidth(100)

	m, _ = m.Update(ContextBudgetMsg{
		Budget: ContextBudgetSnapshot{
			Used:    12400,
			Limit:   128000,
			Percent: 0.096875,
			Turns:   7,
		},
	})

	view := m.View()
	if !strings.Contains(view, "12.4k") {
		t.Errorf("expected view to contain '12.4k', got: %s", view)
	}
	if !strings.Contains(view, "128k") {
		t.Errorf("expected view to contain '128k', got: %s", view)
	}
}

func TestContextBarModel_View_NarrowTerminal(t *testing.T) {
	m := NewContextBarModel()
	m = m.SetWidth(60) // Below 80

	m, _ = m.Update(ContextBudgetMsg{
		Budget: ContextBudgetSnapshot{
			Used:    50000,
			Limit:   128000,
			Percent: 0.390625,
			Turns:   3,
		},
	})

	view := m.View()
	if view == "" {
		t.Error("expected non-empty narrow view")
	}
	// Narrow view should contain turn count but not "compact:" label
	if strings.Contains(view, "compact:") {
		t.Error("narrow view should not show compact label")
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		count int
		want  string
	}{
		{0, "0"},
		{500, "500"},
		{1000, "1k"},
		{1200, "1.2k"},
		{12400, "12.4k"},
		{128000, "128k"},
		{1500000, "1.5M"},
	}
	for _, tt := range tests {
		got := formatTokenCount(tt.count)
		if got != tt.want {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tt.count, got, tt.want)
		}
	}
}

func TestContextBarModel_BudgetColor_Green(t *testing.T) {
	m := NewContextBarModel()
	m.budget = ContextBudgetSnapshot{Percent: 0.30}
	color := m.budgetColor()
	if color != lipgloss.Color(ColorSuccess) {
		t.Errorf("expected green for 30%%, got %v", color)
	}
}

func TestContextBarModel_BudgetColor_Yellow(t *testing.T) {
	m := NewContextBarModel()
	m.budget = ContextBudgetSnapshot{Percent: 0.65}
	color := m.budgetColor()
	if color != lipgloss.Color(ColorWarning) {
		t.Errorf("expected yellow for 65%%, got %v", color)
	}
}

func TestContextBarModel_BudgetColor_Red(t *testing.T) {
	m := NewContextBarModel()
	m.budget = ContextBudgetSnapshot{Percent: 0.90}
	color := m.budgetColor()
	if color != lipgloss.Color(ColorError) {
		t.Errorf("expected red for 90%%, got %v", color)
	}
}
```

---

## Task 4: Permission Prompt

An inline dialog rendered when the permission checker (Plan 4) returns `Ask`. Captures keyboard input and blocks only the requesting tool while allowing the rest of the TUI to remain interactive.

### Steps

- [ ] **4.1** Create `tui/permissions.go` with `PermissionModel` struct
- [ ] **4.2** Implement `Update()` to handle key presses (a/A/d/D) and emit `PermissionResponseMsg`
- [ ] **4.3** Implement `View()` with tool info box, risk level indicator, key options
- [ ] **4.4** Implement "Always" persistence via `permissions.SaveConfig()`
- [ ] **4.5** Write tests in `tui/permissions_test.go`

### Complete Code

**File: `cmd/celeste/tui/permissions.go`** (NEW):

```go
// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the permission prompt dialog component.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PermissionModel renders an inline permission prompt when a tool requires user approval.
// Only one permission prompt is active at a time. When active, it captures a/A/d/D keys.
type PermissionModel struct {
	active       bool
	toolCallID   string
	toolName     string
	inputSummary string
	riskLevel    PermissionRiskLevel
	responseCh   chan PermissionDecision
	width        int
}

// NewPermissionModel creates a new permission model.
func NewPermissionModel() PermissionModel {
	return PermissionModel{}
}

// SetWidth sets the rendering width.
func (m PermissionModel) SetWidth(width int) PermissionModel {
	m.width = width
	return m
}

// IsActive returns true if a permission prompt is currently displayed.
func (m PermissionModel) IsActive() bool {
	return m.active
}

// ActiveToolCallID returns the tool call ID of the current prompt, or empty string.
func (m PermissionModel) ActiveToolCallID() string {
	if !m.active {
		return ""
	}
	return m.toolCallID
}

// Update handles messages for the permission prompt.
func (m PermissionModel) Update(msg tea.Msg) (PermissionModel, tea.Cmd) {
	if !m.active {
		// Only handle activation messages when inactive
		switch msg := msg.(type) {
		case PermissionRequestMsg:
			m.active = true
			m.toolCallID = msg.ToolCallID
			m.toolName = msg.ToolName
			m.inputSummary = msg.InputSummary
			m.riskLevel = msg.RiskLevel
			m.responseCh = msg.ResponseCh
			return m, nil
		}
		return m, nil
	}

	// Active: capture key presses
	switch msg := msg.(type) {
	case tea.KeyMsg:
		var decision PermissionDecision
		var handled bool

		switch msg.String() {
		case "a":
			decision = PermissionAllowOnce
			handled = true
		case "A":
			decision = PermissionAlwaysAllow
			handled = true
		case "d":
			decision = PermissionDenyOnce
			handled = true
		case "D":
			decision = PermissionAlwaysDeny
			handled = true
		}

		if handled {
			// Send decision on the response channel
			if m.responseCh != nil {
				select {
				case m.responseCh <- decision:
				default:
					// Channel full or closed, skip
				}
			}

			// Build response message
			resp := PermissionResponseMsg{
				ToolCallID: m.toolCallID,
				Decision:   decision,
			}

			// Deactivate
			m.active = false
			m.toolCallID = ""
			m.toolName = ""
			m.inputSummary = ""
			m.responseCh = nil

			return m, func() tea.Msg { return resp }
		}
	}

	return m, nil
}

// View renders the permission prompt dialog.
func (m PermissionModel) View() string {
	if !m.active {
		return ""
	}

	// Risk level indicator with color
	var riskIcon, riskLabel string
	var riskStyle lipgloss.Style
	switch m.riskLevel {
	case PermissionRiskHigh:
		riskIcon = "▲▲"
		riskLabel = "HIGH RISK"
		riskStyle = PermissionRiskHighStyle
	case PermissionRiskMedium:
		riskIcon = "▲"
		riskLabel = "MEDIUM RISK"
		riskStyle = PermissionRiskMediumStyle
	default:
		riskIcon = "◇"
		riskLabel = "LOW RISK"
		riskStyle = PermissionRiskLowStyle
	}

	riskStr := riskStyle.Render(fmt.Sprintf("%s %s", riskIcon, riskLabel))

	// Title
	title := PermissionTitleStyle.Render("PERMISSION REQUIRED")

	// Tool info
	toolLine := fmt.Sprintf(
		"%s %s  %s",
		PermissionLabelStyle.Render("tool:"),
		PermissionToolNameStyle.Render(m.toolName),
		riskStr,
	)

	// Input summary (truncated)
	summary := m.inputSummary
	maxLen := m.width - 16
	if maxLen < 20 {
		maxLen = 20
	}
	if len(summary) > maxLen {
		summary = summary[:maxLen-3] + "..."
	}
	inputLine := fmt.Sprintf(
		"%s %s",
		PermissionLabelStyle.Render("input:"),
		PermissionInputStyle.Render(summary),
	)

	// Key options
	options := fmt.Sprintf(
		"%s  %s  %s  %s",
		PermissionKeyStyle.Render("[a]")+" allow once",
		PermissionKeyStyle.Render("[A]")+" always allow",
		PermissionKeyStyle.Render("[d]")+" deny",
		PermissionKeyStyle.Render("[D]")+" always deny",
	)
	optionsLine := PermissionOptionsStyle.Render(options)

	// Compose the dialog box
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		toolLine,
		inputLine,
		"",
		optionsLine,
	)

	return PermissionBoxStyle.
		Width(m.width - 4).
		Render(content)
}
```

**File: `cmd/celeste/tui/permissions_test.go`** (NEW):

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPermissionModel_InactiveByDefault(t *testing.T) {
	m := NewPermissionModel()
	if m.IsActive() {
		t.Error("expected inactive by default")
	}
	if m.View() != "" {
		t.Error("expected empty view when inactive")
	}
}

func TestPermissionModel_ActivateOnRequest(t *testing.T) {
	m := NewPermissionModel()
	m = m.SetWidth(100)

	ch := make(chan PermissionDecision, 1)
	m, _ = m.Update(PermissionRequestMsg{
		ToolCallID:   "call_1",
		ToolName:     "bash",
		InputSummary: "rm -rf /tmp/test",
		RiskLevel:    PermissionRiskHigh,
		ResponseCh:   ch,
	})

	if !m.IsActive() {
		t.Error("expected active after request")
	}
	if m.ActiveToolCallID() != "call_1" {
		t.Errorf("expected call_1, got %s", m.ActiveToolCallID())
	}
}

func TestPermissionModel_AllowOnce(t *testing.T) {
	m := NewPermissionModel()
	m = m.SetWidth(100)

	ch := make(chan PermissionDecision, 1)
	m, _ = m.Update(PermissionRequestMsg{
		ToolCallID:   "call_1",
		ToolName:     "bash",
		InputSummary: "ls",
		RiskLevel:    PermissionRiskLow,
		ResponseCh:   ch,
	})

	// Press 'a' for allow once
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	if m.IsActive() {
		t.Error("expected inactive after response")
	}

	// Check channel
	select {
	case decision := <-ch:
		if decision != PermissionAllowOnce {
			t.Errorf("expected PermissionAllowOnce, got %d", decision)
		}
	default:
		t.Error("expected decision on channel")
	}

	// Check command produces PermissionResponseMsg
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	resp, ok := msg.(PermissionResponseMsg)
	if !ok {
		t.Fatalf("expected PermissionResponseMsg, got %T", msg)
	}
	if resp.ToolCallID != "call_1" {
		t.Errorf("expected call_1, got %s", resp.ToolCallID)
	}
	if resp.Decision != PermissionAllowOnce {
		t.Errorf("expected PermissionAllowOnce, got %d", resp.Decision)
	}
}

func TestPermissionModel_AlwaysDeny(t *testing.T) {
	m := NewPermissionModel()
	m = m.SetWidth(100)

	ch := make(chan PermissionDecision, 1)
	m, _ = m.Update(PermissionRequestMsg{
		ToolCallID:   "call_2",
		ToolName:     "write_file",
		InputSummary: "/etc/passwd",
		RiskLevel:    PermissionRiskHigh,
		ResponseCh:   ch,
	})

	// Press 'D' for always deny
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})

	decision := <-ch
	if decision != PermissionAlwaysDeny {
		t.Errorf("expected PermissionAlwaysDeny, got %d", decision)
	}
}

func TestPermissionModel_IgnoresUnrelatedKeys(t *testing.T) {
	m := NewPermissionModel()
	m = m.SetWidth(100)

	ch := make(chan PermissionDecision, 1)
	m, _ = m.Update(PermissionRequestMsg{
		ToolCallID:   "call_3",
		ToolName:     "bash",
		InputSummary: "echo hello",
		RiskLevel:    PermissionRiskLow,
		ResponseCh:   ch,
	})

	// Press 'x' - should be ignored
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	if !m.IsActive() {
		t.Error("expected still active after unrelated key")
	}
	if cmd != nil {
		t.Error("expected nil cmd for unrelated key")
	}
}

func TestPermissionModel_View_ContainsToolInfo(t *testing.T) {
	m := NewPermissionModel()
	m = m.SetWidth(100)

	ch := make(chan PermissionDecision, 1)
	m, _ = m.Update(PermissionRequestMsg{
		ToolCallID:   "call_4",
		ToolName:     "bash",
		InputSummary: "curl https://example.com",
		RiskLevel:    PermissionRiskMedium,
		ResponseCh:   ch,
	})

	view := m.View()
	if !strings.Contains(view, "PERMISSION REQUIRED") {
		t.Error("expected title in view")
	}
	if !strings.Contains(view, "bash") {
		t.Error("expected tool name in view")
	}
	if !strings.Contains(view, "curl") {
		t.Error("expected input summary in view")
	}
}

func TestPermissionModel_View_HighRisk(t *testing.T) {
	m := NewPermissionModel()
	m = m.SetWidth(100)

	ch := make(chan PermissionDecision, 1)
	m, _ = m.Update(PermissionRequestMsg{
		ToolCallID:   "call_5",
		ToolName:     "bash",
		InputSummary: "rm -rf /",
		RiskLevel:    PermissionRiskHigh,
		ResponseCh:   ch,
	})

	view := m.View()
	if !strings.Contains(view, "HIGH RISK") {
		t.Error("expected HIGH RISK label in view")
	}
}

func TestPermissionModel_RequestWhileInactive(t *testing.T) {
	m := NewPermissionModel()

	// Keys while inactive should be no-ops
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil {
		t.Error("expected nil cmd when inactive")
	}
}
```

---

## Task 5: MCP Panel

A full-screen panel (similar to `CollectionsModel`) for browsing MCP servers and their tools. Accessed via the `/mcp` command.

### Steps

- [ ] **5.1** Create `tui/mcp_panel.go` with `MCPPanelModel` struct
- [ ] **5.2** Implement list view: server name, health dot, tool count, transport
- [ ] **5.3** Implement drill-down view: Enter on a server shows its tools
- [ ] **5.4** Implement navigation: arrow keys, Esc to close/back
- [ ] **5.5** Write tests in `tui/mcp_panel_test.go`

### Complete Code

**File: `cmd/celeste/tui/mcp_panel.go`** (NEW):

```go
// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the MCP server and tool browser panel.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// mcpViewMode tracks whether we are viewing the server list or a single server's tools.
type mcpViewMode int

const (
	mcpViewServers mcpViewMode = iota
	mcpViewTools
)

// MCPPanelModel is the TUI model for browsing MCP servers and their tools.
// Similar in structure to CollectionsModel: viewport-based, cursor navigation.
type MCPPanelModel struct {
	servers       []MCPServerInfo
	cursor        int
	viewMode      mcpViewMode
	selectedServer int         // Index of server when drilling into tools
	toolCursor    int          // Cursor for tool list
	viewport      viewport.Model
	width, height int
	err           error
}

// NewMCPPanelModel creates a new MCP panel model.
func NewMCPPanelModel() MCPPanelModel {
	return MCPPanelModel{
		viewport: viewport.New(80, 20),
	}
}

// SetSize sets the panel dimensions.
func (m MCPPanelModel) SetSize(width, height int) MCPPanelModel {
	m.width = width
	m.height = height
	m.viewport.Width = width - 4
	m.viewport.Height = height - 6 // Room for header and footer
	m.updateContent()
	return m
}

// SetServers updates the server list.
func (m MCPPanelModel) SetServers(servers []MCPServerInfo) MCPPanelModel {
	m.servers = servers
	m.updateContent()
	return m
}

// Init implements partial tea.Model.
func (m MCPPanelModel) Init() tea.Cmd {
	return nil
}

// Update handles messages for the MCP panel.
func (m MCPPanelModel) Update(msg tea.Msg) (MCPPanelModel, tea.Cmd) {
	switch msg := msg.(type) {
	case MCPStatusMsg:
		m.servers = msg.Servers
		m.updateContent()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "esc":
			if m.viewMode == mcpViewTools {
				// Back to server list
				m.viewMode = mcpViewServers
				m.toolCursor = 0
				m.updateContent()
				return m, nil
			}
			// Close panel entirely — return nil cmd, caller checks viewMode
			return m, nil

		case "up", "k":
			if m.viewMode == mcpViewServers {
				if m.cursor > 0 {
					m.cursor--
					m.updateContent()
				}
			} else {
				if m.toolCursor > 0 {
					m.toolCursor--
					m.updateContent()
				}
			}

		case "down", "j":
			if m.viewMode == mcpViewServers {
				if m.cursor < len(m.servers)-1 {
					m.cursor++
					m.updateContent()
				}
			} else {
				server := m.servers[m.selectedServer]
				if m.toolCursor < len(server.Tools)-1 {
					m.toolCursor++
					m.updateContent()
				}
			}

		case "enter":
			if m.viewMode == mcpViewServers && m.cursor < len(m.servers) {
				m.selectedServer = m.cursor
				m.viewMode = mcpViewTools
				m.toolCursor = 0
				m.updateContent()
			}
		}
	}

	return m, nil
}

// View renders the MCP panel.
func (m MCPPanelModel) View() string {
	// Header
	title := MCPPanelTitleStyle.Render("MCP Servers")
	var subtitle string
	if m.viewMode == mcpViewTools && m.selectedServer < len(m.servers) {
		server := m.servers[m.selectedServer]
		subtitle = MCPPanelSubtitleStyle.Render(
			fmt.Sprintf("  %s  [%s]", server.Name, server.Transport),
		)
	}

	header := lipgloss.JoinHorizontal(lipgloss.Top, title, subtitle)

	// Footer
	var footerText string
	if m.viewMode == mcpViewServers {
		footerText = "↑/↓: Navigate  Enter: View tools  Esc: Close"
	} else {
		footerText = "↑/↓: Navigate  Esc: Back"
	}
	footer := MCPPanelFooterStyle.Render(footerText)

	// Content
	content := m.viewport.View()

	return MCPPanelStyle.
		Width(m.width).
		Height(m.height).
		Render(
			lipgloss.JoinVertical(lipgloss.Left, header, "", content, "", footer),
		)
}

// updateContent rebuilds the viewport content based on current state.
func (m *MCPPanelModel) updateContent() {
	switch m.viewMode {
	case mcpViewServers:
		m.viewport.SetContent(m.renderServerList())
	case mcpViewTools:
		m.viewport.SetContent(m.renderToolList())
	}
}

// renderServerList renders the server list view.
func (m MCPPanelModel) renderServerList() string {
	if len(m.servers) == 0 {
		return TextMutedStyle.Render("  No MCP servers configured.\n\n  Add servers in ~/.celeste/mcp.json")
	}

	var lines []string
	for i, server := range m.servers {
		line := m.renderServerLine(i, server)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// renderServerLine renders a single server entry.
func (m MCPPanelModel) renderServerLine(index int, server MCPServerInfo) string {
	// Cursor indicator
	cursor := "  "
	if index == m.cursor {
		cursor = MCPPanelCursorStyle.Render("> ")
	}

	// Health dot
	var healthDot string
	if server.Connected {
		healthDot = MCPPanelConnectedStyle.Render("●")
	} else {
		healthDot = MCPPanelDisconnectedStyle.Render("○")
	}

	// Server name
	nameStyle := MCPPanelServerNameStyle
	if index == m.cursor {
		nameStyle = nameStyle.Bold(true)
	}
	name := nameStyle.Render(server.Name)

	// Transport badge
	transport := MCPPanelTransportStyle.Render(fmt.Sprintf("[%s]", server.Transport))

	// Tool count
	toolCount := MCPPanelToolCountStyle.Render(fmt.Sprintf("%d tools", server.ToolCount))

	// Error (if any)
	var errorStr string
	if server.LastError != "" {
		errMsg := server.LastError
		if len(errMsg) > 40 {
			errMsg = errMsg[:37] + "..."
		}
		errorStr = "  " + MCPPanelErrorStyle.Render(errMsg)
	}

	line := fmt.Sprintf("%s%s %s %s  %s", cursor, healthDot, name, transport, toolCount)
	if errorStr != "" {
		line += "\n" + strings.Repeat(" ", 4) + errorStr
	}

	return line
}

// renderToolList renders the tool list for the selected server.
func (m MCPPanelModel) renderToolList() string {
	if m.selectedServer >= len(m.servers) {
		return ""
	}

	server := m.servers[m.selectedServer]
	if len(server.Tools) == 0 {
		return TextMutedStyle.Render("  No tools available from this server.")
	}

	var lines []string
	for i, tool := range server.Tools {
		cursor := "  "
		if i == m.toolCursor {
			cursor = MCPPanelCursorStyle.Render("> ")
		}

		nameStyle := MCPPanelToolNameStyle
		if i == m.toolCursor {
			nameStyle = nameStyle.Bold(true)
		}
		name := nameStyle.Render(tool.Name)

		desc := ""
		if tool.Description != "" {
			d := tool.Description
			maxLen := m.width - 30
			if maxLen < 20 {
				maxLen = 20
			}
			if len(d) > maxLen {
				d = d[:maxLen-3] + "..."
			}
			desc = MCPPanelToolDescStyle.Render("  " + d)
		}

		lines = append(lines, fmt.Sprintf("%s%s%s", cursor, name, desc))
	}

	return strings.Join(lines, "\n")
}
```

**File: `cmd/celeste/tui/mcp_panel_test.go`** (NEW):

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestMCPPanel() MCPPanelModel {
	m := NewMCPPanelModel()
	m = m.SetSize(100, 30)
	m = m.SetServers([]MCPServerInfo{
		{
			Name:      "filesystem",
			Transport: "stdio",
			Connected: true,
			ToolCount: 5,
			Tools: []MCPToolInfo{
				{Name: "read_file", Description: "Read a file from disk"},
				{Name: "write_file", Description: "Write content to a file"},
				{Name: "list_dir", Description: "List directory contents"},
				{Name: "search", Description: "Search for text in files"},
				{Name: "stat", Description: "Get file metadata"},
			},
		},
		{
			Name:      "database",
			Transport: "sse",
			Connected: false,
			ToolCount: 0,
			LastError: "connection refused",
		},
		{
			Name:      "web-search",
			Transport: "streamable-http",
			Connected: true,
			ToolCount: 2,
			Tools: []MCPToolInfo{
				{Name: "search_web", Description: "Search the web"},
				{Name: "fetch_url", Description: "Fetch content from a URL"},
			},
		},
	})
	return m
}

func TestMCPPanelModel_EmptyServers(t *testing.T) {
	m := NewMCPPanelModel()
	m = m.SetSize(100, 30)

	view := m.View()
	if !strings.Contains(view, "No MCP servers") {
		t.Error("expected empty state message")
	}
}

func TestMCPPanelModel_ServerListView(t *testing.T) {
	m := newTestMCPPanel()

	view := m.View()
	if !strings.Contains(view, "filesystem") {
		t.Error("expected filesystem server in view")
	}
	if !strings.Contains(view, "database") {
		t.Error("expected database server in view")
	}
	if !strings.Contains(view, "web-search") {
		t.Error("expected web-search server in view")
	}
	if !strings.Contains(view, "5 tools") {
		t.Error("expected tool count in view")
	}
}

func TestMCPPanelModel_Navigation(t *testing.T) {
	m := newTestMCPPanel()

	if m.cursor != 0 {
		t.Errorf("expected cursor at 0, got %d", m.cursor)
	}

	// Move down
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursor != 1 {
		t.Errorf("expected cursor at 1, got %d", m.cursor)
	}

	// Move down again
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 2 {
		t.Errorf("expected cursor at 2, got %d", m.cursor)
	}

	// Move down past end (should stay)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 2 {
		t.Errorf("expected cursor still at 2, got %d", m.cursor)
	}

	// Move up
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.cursor != 1 {
		t.Errorf("expected cursor at 1, got %d", m.cursor)
	}
}

func TestMCPPanelModel_DrillDown(t *testing.T) {
	m := newTestMCPPanel()

	// Enter on first server
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.viewMode != mcpViewTools {
		t.Error("expected tools view mode after Enter")
	}
	if m.selectedServer != 0 {
		t.Errorf("expected selected server 0, got %d", m.selectedServer)
	}

	view := m.View()
	if !strings.Contains(view, "read_file") {
		t.Error("expected read_file tool in drilled-down view")
	}
	if !strings.Contains(view, "write_file") {
		t.Error("expected write_file tool in drilled-down view")
	}
}

func TestMCPPanelModel_DrillDown_BackWithEsc(t *testing.T) {
	m := newTestMCPPanel()

	// Drill in
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.viewMode != mcpViewTools {
		t.Fatal("expected tools view")
	}

	// Press Esc to go back
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.viewMode != mcpViewServers {
		t.Error("expected servers view after Esc from tools")
	}
}

func TestMCPPanelModel_ToolNavigation(t *testing.T) {
	m := newTestMCPPanel()

	// Drill into filesystem server (5 tools)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.toolCursor != 0 {
		t.Errorf("expected tool cursor at 0, got %d", m.toolCursor)
	}

	// Navigate down through tools
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.toolCursor != 1 {
		t.Errorf("expected tool cursor at 1, got %d", m.toolCursor)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.toolCursor != 4 {
		t.Errorf("expected tool cursor at 4, got %d", m.toolCursor)
	}

	// Past end
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.toolCursor != 4 {
		t.Errorf("expected tool cursor still at 4, got %d", m.toolCursor)
	}
}

func TestMCPPanelModel_MCPStatusMsg(t *testing.T) {
	m := NewMCPPanelModel()
	m = m.SetSize(100, 30)

	m, _ = m.Update(MCPStatusMsg{
		Servers: []MCPServerInfo{
			{Name: "test-server", Transport: "stdio", Connected: true, ToolCount: 3},
		},
	})

	if len(m.servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(m.servers))
	}
	if m.servers[0].Name != "test-server" {
		t.Errorf("expected test-server, got %s", m.servers[0].Name)
	}
}

func TestMCPPanelModel_DisconnectedServerShowsError(t *testing.T) {
	m := NewMCPPanelModel()
	m = m.SetSize(100, 30)
	m = m.SetServers([]MCPServerInfo{
		{
			Name:      "broken",
			Transport: "sse",
			Connected: false,
			ToolCount: 0,
			LastError: "ECONNREFUSED",
		},
	})

	view := m.View()
	if !strings.Contains(view, "ECONNREFUSED") {
		t.Error("expected error message in view for disconnected server")
	}
}
```

---

## Task 6: Wire Components into AppModel

Modify `cmd/celeste/tui/app.go` to integrate all 4 new components into the existing `AppModel` state machine.

### Steps

- [ ] **6.1** Add new component fields to `AppModel` struct
- [ ] **6.2** Initialize new components in `NewApp()`
- [ ] **6.3** Route new message types in `Update()`
- [ ] **6.4** Add context bar to `View()` layout (between chat and input)
- [ ] **6.5** Add tool progress inline in chat area
- [ ] **6.6** Handle permission prompt blocking (overlay on input)
- [ ] **6.7** Add `/mcp` command routing and `"mcp"` view mode
- [ ] **6.8** Pass resize events to new components
- [ ] **6.9** Add `"mcp"` to `knownCommands` in `input.go`
- [ ] **6.10** Wire `PermissionResponseMsg` to resume/abort blocked tool calls

### Key Code Changes

**6.1 -- New fields in `AppModel` struct** (add after existing `splitPanel` fields in `app.go`):

```go
// Plan 6: New TUI components
toolProgress    ToolProgressModel
contextBar      ContextBarModel
permissionPrompt PermissionModel
mcpPanel        *MCPPanelModel
```

**6.2 -- Initialize in `NewApp()`** (add to `NewApp` return struct):

```go
func NewApp(llmClient LLMClient) AppModel {
	return AppModel{
		header:            NewHeaderModel(),
		chat:              NewChatModel(),
		input:             NewInputModel(),
		skills:            NewSkillsModel(),
		status:            NewStatusModel(),
		toolProgress:      NewToolProgressModel(),      // Plan 6
		contextBar:        NewContextBarModel(),         // Plan 6
		permissionPrompt:  NewPermissionModel(),         // Plan 6
		llmClient:         llmClient,
		viewMode:          "chat",
		runtimeMode:       config.RuntimeModeClassic,
		clawMaxIterations: config.DefaultClawMaxToolIterations,
	}
}
```

**6.3 -- Route new messages in `Update()`** (add cases in the main `switch msg.(type)` block):

```go
case ToolProgressMsg:
	m.toolProgress, cmd = m.toolProgress.Update(msg)
	cmds = append(cmds, cmd)

case ContextBudgetMsg:
	m.contextBar, cmd = m.contextBar.Update(msg)
	cmds = append(cmds, cmd)

case PermissionRequestMsg:
	m.permissionPrompt, cmd = m.permissionPrompt.Update(msg)
	cmds = append(cmds, cmd)

case PermissionResponseMsg:
	// Resume or abort the blocked tool call
	cmds = append(cmds, m.handlePermissionResponse(msg))

case MCPStatusMsg:
	if m.mcpPanel != nil {
		*m.mcpPanel, cmd = m.mcpPanel.Update(msg)
		cmds = append(cmds, cmd)
	}
```

**6.3a -- Handle permission prompt key capture** (in the `tea.KeyMsg` handler, add early return when prompt is active):

```go
case tea.KeyMsg:
	// If permission prompt is active, it captures keys first
	if m.permissionPrompt.IsActive() {
		m.permissionPrompt, cmd = m.permissionPrompt.Update(msg)
		return m, cmd
	}
	// ... existing key handling ...
```

**6.4 -- Update `View()` layout** (modify the vertical composition):

```go
// Build the layout vertically
var sections []string

// Header (fixed, 1 line)
sections = append(sections, m.header.View())

// Chat panel (flexible height)
sections = append(sections, m.chat.View())

// Tool progress indicators (variable height, between chat and context bar)
if !m.toolProgress.IsEmpty() {
	sections = append(sections, m.toolProgress.View())
}

// Permission prompt (if active, between chat and input)
if m.permissionPrompt.IsActive() {
	sections = append(sections, m.permissionPrompt.View())
}

// Context budget bar (fixed, 1 line)
contextBarView := m.contextBar.View()
if contextBarView != "" {
	sections = append(sections, contextBarView)
}

// Input panel (fixed, 3 lines)
sections = append(sections, m.input.View())

// ... skills panel, status bar ...
```

**6.5 -- Add `/mcp` command routing** (in the command dispatch section of `Update()`):

```go
case "mcp":
	if m.mcpPanel == nil {
		panel := NewMCPPanelModel()
		m.mcpPanel = &panel
	}
	*m.mcpPanel = m.mcpPanel.SetSize(m.width, m.height)
	m.viewMode = "mcp"
	// Request fresh status from MCP manager
	// (The MCP manager integration sends MCPStatusMsg asynchronously)
```

**6.6 -- Add `"mcp"` view mode to `View()`** (add after existing view mode checks):

```go
// Show MCP panel if in that mode
if m.viewMode == "mcp" && m.mcpPanel != nil {
	return m.mcpPanel.View()
}
```

**6.7 -- Handle resize for new components** (in the `tea.WindowSizeMsg` handler):

```go
case tea.WindowSizeMsg:
	m.width = msg.Width
	m.height = msg.Height
	// ... existing resize logic ...
	m.toolProgress = m.toolProgress.SetWidth(msg.Width)
	m.contextBar = m.contextBar.SetWidth(msg.Width)
	m.permissionPrompt = m.permissionPrompt.SetWidth(msg.Width)
	if m.mcpPanel != nil {
		*m.mcpPanel = m.mcpPanel.SetSize(msg.Width, msg.Height)
	}
```

**6.8 -- Handle PermissionResponseMsg** (new method on AppModel):

```go
// handlePermissionResponse resumes or aborts a tool call after user decision.
func (m *AppModel) handlePermissionResponse(msg PermissionResponseMsg) tea.Cmd {
	switch msg.Decision {
	case PermissionAllowOnce, PermissionAlwaysAllow:
		// Find the pending tool call and execute it
		for _, ptc := range m.pendingToolCalls {
			if ptc.toolCallID == msg.ToolCallID {
				return m.llmClient.ExecuteSkill(ptc.name, ptc.args, ptc.toolCallID)
			}
		}
	case PermissionDenyOnce, PermissionAlwaysDeny:
		// Send a denial result back to the LLM
		return func() tea.Msg {
			return SkillResultMsg{
				Name:       "",
				Result:     "Permission denied by user",
				ToolCallID: msg.ToolCallID,
			}
		}
	}
	return nil
}
```

**6.9 -- Forward TickMsg to tool progress** (in existing TickMsg handler):

```go
case TickMsg:
	// ... existing tick handling ...
	m.toolProgress, cmd = m.toolProgress.Update(msg)
	cmds = append(cmds, cmd)
```

**6.10 -- Clear tool progress on new user message** (in SendMessageMsg handler):

```go
case SendMessageMsg:
	m.toolProgress = m.toolProgress.Clear()
	// ... existing handling ...
```

### Modification to `input.go`

Add `"mcp"` to the `knownCommands` slice:

```go
var knownCommands = []string{
	"agent", "clear", "config", "context", "endpoint", "export",
	"help", "mcp", "model", "nsfw", "orch", "orchestrate", "providers",
	"safe", "session", "set-model", "skills", "stats", "tools",
}
```

---

## Task 7: Update Styles

Add new styles to `cmd/celeste/tui/styles.go` for all 4 new components.

### Steps

- [ ] **7.1** Add tool progress styles
- [ ] **7.2** Add context bar styles
- [ ] **7.3** Add permission prompt styles
- [ ] **7.4** Add MCP panel styles

### Complete Code

Append the following to the component-specific styles section in `styles.go`:

```go
// --- Plan 6: New component styles ---

// Tool progress indicator styles
var (
	ToolProgressBoxStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(ColorBorderPurple).
		Padding(0, 1).
		MarginLeft(2)

	ToolProgressSpinnerStyle = lipgloss.NewStyle().
		Foreground(ColorWarning).
		Bold(true)

	ToolProgressDoneStyle = lipgloss.NewStyle().
		Foreground(ColorSuccess).
		Bold(true)

	ToolProgressFailedStyle = lipgloss.NewStyle().
		Foreground(ColorError).
		Bold(true)

	ToolProgressNameStyle = lipgloss.NewStyle().
		Foreground(ColorPurpleNeon).
		Bold(true)

	ToolProgressElapsedStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	ToolProgressMessageStyle = lipgloss.NewStyle().
		Foreground(ColorTextSecondary)

	ToolProgressBarBracketStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	ToolProgressBarFillStyle = lipgloss.NewStyle().
		Foreground(ColorPurple)
)

// Context budget bar styles
var (
	ContextBarStyle = lipgloss.NewStyle().
		Background(ColorBgGlass).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderBottom(true).
		BorderForeground(ColorBorder).
		Padding(0, 1)

	ContextBarLabelStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	ContextBarValueStyle = lipgloss.NewStyle().
		Foreground(ColorTextSecondary)

	ContextBarSepStyle = lipgloss.NewStyle().
		Foreground(ColorBorder)

	ContextBarDimStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted)
)

// Permission prompt styles
var (
	PermissionBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorWarning).
		Padding(1, 2).
		MarginLeft(2).
		MarginRight(2)

	PermissionTitleStyle = lipgloss.NewStyle().
		Foreground(ColorWarning).
		Bold(true)

	PermissionLabelStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	PermissionToolNameStyle = lipgloss.NewStyle().
		Foreground(ColorPurpleNeon).
		Bold(true)

	PermissionInputStyle = lipgloss.NewStyle().
		Foreground(ColorTextSecondary)

	PermissionKeyStyle = lipgloss.NewStyle().
		Foreground(ColorCyan).
		Bold(true)

	PermissionOptionsStyle = lipgloss.NewStyle().
		Foreground(ColorTextSecondary)

	PermissionRiskHighStyle = lipgloss.NewStyle().
		Foreground(ColorError).
		Bold(true)

	PermissionRiskMediumStyle = lipgloss.NewStyle().
		Foreground(ColorWarning).
		Bold(true)

	PermissionRiskLowStyle = lipgloss.NewStyle().
		Foreground(ColorSuccess)
)

// MCP panel styles
var (
	MCPPanelStyle = lipgloss.NewStyle().
		Padding(1, 2)

	MCPPanelTitleStyle = lipgloss.NewStyle().
		Foreground(ColorAccentGlow).
		Bold(true).
		MarginBottom(1)

	MCPPanelSubtitleStyle = lipgloss.NewStyle().
		Foreground(ColorTextSecondary)

	MCPPanelFooterStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Italic(true)

	MCPPanelCursorStyle = lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	MCPPanelConnectedStyle = lipgloss.NewStyle().
		Foreground(ColorSuccess)

	MCPPanelDisconnectedStyle = lipgloss.NewStyle().
		Foreground(ColorError)

	MCPPanelServerNameStyle = lipgloss.NewStyle().
		Foreground(ColorText)

	MCPPanelTransportStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	MCPPanelToolCountStyle = lipgloss.NewStyle().
		Foreground(ColorPurpleNeon)

	MCPPanelErrorStyle = lipgloss.NewStyle().
		Foreground(ColorError).
		Italic(true)

	MCPPanelToolNameStyle = lipgloss.NewStyle().
		Foreground(ColorCyanLight)

	MCPPanelToolDescStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted)
)
```

---

## Task 8: Final Verification

Build and manually test all 4 new components.

### Steps

- [ ] **8.1** Run `go build ./cmd/celeste/...` and fix any compilation errors
- [ ] **8.2** Run `go test ./cmd/celeste/tui/...` and verify all tests pass
- [ ] **8.3** Run `go vet ./cmd/celeste/tui/...` and fix any issues
- [ ] **8.4** Launch TUI, verify context bar appears after first LLM response
- [ ] **8.5** Trigger a tool call, verify tool progress indicators render correctly
- [ ] **8.6** Test permission prompt by configuring a tool to require `Ask` permission
- [ ] **8.7** Run `/mcp` command, verify panel renders server list
- [ ] **8.8** Test narrow terminal (<80 cols) -- context bar should collapse, other components should degrade gracefully
- [ ] **8.9** Test concurrent tool executions render stacked
- [ ] **8.10** Verify collapsed tool entries shrink after 2s delay

### Verification Commands

```bash
# Build
go build ./cmd/celeste/...

# Run all TUI tests
go test -v ./cmd/celeste/tui/...

# Vet
go vet ./cmd/celeste/tui/...

# Run with race detector
go test -race ./cmd/celeste/tui/...
```

---

## Summary of All Files Changed

| File | Action | Description |
|------|--------|-------------|
| `cmd/celeste/tui/messages.go` | MODIFY | Add 5 new message types, 3 enums |
| `cmd/celeste/tui/messages_test.go` | NEW | Tests for new message types |
| `cmd/celeste/tui/tool_progress.go` | NEW | Tool progress indicator component (~200 lines) |
| `cmd/celeste/tui/tool_progress_test.go` | NEW | Tests for tool progress (~170 lines) |
| `cmd/celeste/tui/context_bar.go` | NEW | Context budget status bar (~160 lines) |
| `cmd/celeste/tui/context_bar_test.go` | NEW | Tests for context bar (~100 lines) |
| `cmd/celeste/tui/permissions.go` | NEW | Permission prompt dialog (~180 lines) |
| `cmd/celeste/tui/permissions_test.go` | NEW | Tests for permission prompt (~160 lines) |
| `cmd/celeste/tui/mcp_panel.go` | NEW | MCP server/tool browser (~250 lines) |
| `cmd/celeste/tui/mcp_panel_test.go` | NEW | Tests for MCP panel (~170 lines) |
| `cmd/celeste/tui/styles.go` | MODIFY | Add ~120 lines of new component styles |
| `cmd/celeste/tui/app.go` | MODIFY | Wire 4 components, add fields, route messages, update View() |
| `cmd/celeste/tui/input.go` | MODIFY | Add "mcp" to knownCommands |

**Estimated total new/modified code:** ~1,600 lines (including tests)
