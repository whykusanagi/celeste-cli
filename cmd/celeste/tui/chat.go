// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the chat panel component with scrollable viewport.
package tui

import (
	"fmt"
	"sort"
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
	typingActive   bool // Skip Glamour for the last assistant message during typing
}

// NewChatModel creates a new chat model.
func NewChatModel() ChatModel {
	return ChatModel{
		messages:       []ChatMessage{},
		functionCalls:  []FunctionCall{},
		showSkillCalls: false, // Hidden by default for cleaner UI
	}
}

// TextWidth is the width a message's text wraps at: the chat's content
// width (updateContent) less renderMessageOpt's own margin.
func (m ChatModel) TextWidth() int {
	return m.width - 6
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

// shrunk is the panel h rows tall for one frame, without re-rendering its
// content: View uses it to make room for a modal. A panel following the
// conversation keeps its newest lines in view.
func (m ChatModel) shrunk(h int) ChatModel {
	if !m.ready || h >= m.height {
		return m
	}
	atBottom := m.viewport.AtBottom()
	m.height = h
	m.viewport.Height = h
	if atBottom {
		m.viewport.GotoBottom()
	}
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

// AddHiddenUserMessage adds a user-role message that the LLM sees but
// the chat UI does not render. Used for directives like identity changes.
func (m ChatModel) AddHiddenUserMessage(content string) ChatModel {
	m.messages = append(m.messages, ChatMessage{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
		Metadata:  map[string]any{"hidden": true},
	})
	// No updateContent/GotoBottom — invisible to the user
	return m
}

// Metadata keys recording a user message's UserPromptSubmit result (2.0 F0).
const (
	MetaPromptHookDone = "prompt_hook_done"
	MetaHookContext    = "hook_context"
)

// findUnhookedUser returns the index of the first user message with this
// content and timestamp that has not passed its UserPromptSubmit hooks, or
// -1.
func (m ChatModel) findUnhookedUser(content string, ts time.Time) int {
	for i, msg := range m.messages {
		if msg.Role != "user" || msg.Content != content || !msg.Timestamp.Equal(ts) {
			continue
		}
		if done, _ := msg.Metadata[MetaPromptHookDone].(bool); !done {
			return i
		}
	}
	return -1
}

// DropUser removes the unchecked user message with this content and
// timestamp (a prompt a hook blocked).
func (m ChatModel) DropUser(content string, ts time.Time) ChatModel {
	i := m.findUnhookedUser(content, ts)
	if i < 0 {
		return m
	}
	msgs := make([]ChatMessage, 0, len(m.messages)-1)
	msgs = append(msgs, m.messages[:i]...)
	m.messages = append(msgs, m.messages[i+1:]...)
	m.updateContent()
	return m
}

// MetaUnanswered marks a prompt whose turn failed before anything followed
// it (L5). Sending the same text again retries that prompt instead of
// adding a second copy.
const MetaUnanswered = "unanswered"

// lastPrompt returns the index of the last LLM message, skipping an empty
// reply bubble, when it is a visible user prompt; otherwise -1.
func (m ChatModel) lastPrompt() int {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.Role == "system" || isCompacted(msg) || IsEmptyReply(msg) {
			continue
		}
		if hidden, _ := msg.Metadata["hidden"].(bool); msg.Role != "user" || hidden {
			return -1
		}
		return i
	}
	return -1
}

// withMeta returns m with message i's metadata key set to v (nil deletes
// it). The map is copied: the loop's history may share it.
func (m ChatModel) withMeta(i int, key string, v any) ChatModel {
	msgs := append([]ChatMessage(nil), m.messages...)
	meta := make(map[string]any, len(msgs[i].Metadata)+1)
	for k, val := range msgs[i].Metadata {
		meta[k] = val
	}
	if v == nil {
		delete(meta, key)
	} else {
		meta[key] = v
	}
	msgs[i].Metadata = meta
	m.messages = msgs
	return m
}

// MarkUnanswered marks the last prompt unanswered when a failed turn added
// nothing after it, and reports whether it did.
func (m ChatModel) MarkUnanswered() (ChatModel, bool) {
	i := m.lastPrompt()
	if i < 0 {
		return m, false
	}
	return m.withMeta(i, MetaUnanswered, true), true
}

// LastPromptPlanned reports whether the last prompt went out with the
// hidden plan-mode instruction before it.
func (m ChatModel) LastPromptPlanned() bool {
	i := m.lastPrompt()
	if i < 1 {
		return false
	}
	prev := m.messages[i-1]
	hidden, _ := prev.Metadata["hidden"].(bool)
	return hidden && prev.Role == "user" && prev.Content == PlanModeInstruction
}

// ReuseUnanswered reports whether the last prompt is an unanswered one with
// this content; if so it clears the mark, so the retry sends that prompt
// rather than a copy of it.
func (m ChatModel) ReuseUnanswered(content string) (ChatModel, bool) {
	i := m.lastPrompt()
	if i < 0 || m.messages[i].Content != content {
		return m, false
	}
	if u, _ := m.messages[i].Metadata[MetaUnanswered].(bool); !u {
		return m, false
	}
	return m.withMeta(i, MetaUnanswered, nil), true
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

// AddPlainSystemMessage adds a system message shown as written, without
// markdown: for listings whose placeholders and line breaks must survive.
func (m ChatModel) AddPlainSystemMessage(content string) ChatModel {
	m.messages = append(m.messages, ChatMessage{
		Role:      "system",
		Content:   content,
		Timestamp: time.Now(),
		plain:     true,
	})
	m.updateContent()
	m.viewport.GotoBottom()
	return m
}

// SetTypingActive marks whether the typing animation is running.
// When true, the last assistant message skips Glamour markdown rendering
// so that ANSI-styled corruption glyphs at the cursor don't break the layout.
func (m ChatModel) SetTypingActive(active bool) ChatModel {
	m.typingActive = active
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

// UpdateFunctionResult records the result of the executing call with this
// tool call ID, so parallel calls to the same tool never swap results. An
// empty id falls back to the latest executing call with this name.
func (m ChatModel) UpdateFunctionResult(id, name, result string) ChatModel {
	for i := len(m.functionCalls) - 1; i >= 0; i-- {
		c := m.functionCalls[i]
		if c.Status != "executing" {
			continue
		}
		if (id != "" && c.ID == id) || (id == "" && c.Name == name) {
			m.functionCalls[i].Result = result
			m.functionCalls[i].Status = "completed"
			break
		}
	}
	m.updateContent()
	if m.showSkillCalls && !m.userScrolled {
		m.viewport.GotoBottom()
	}
	return m
}

// GetMessages returns all chat messages (including UI-only system messages).
func (m ChatModel) GetMessages() []ChatMessage {
	return m.messages
}

// ReplaceToolResults swaps the content of tool results by tool call ID; used
// by context compaction to replace pruned results with placeholders.
func (m ChatModel) ReplaceToolResults(edits map[string]string) ChatModel {
	m.messages = EditToolResults(m.messages, edits)
	return m
}

// GetLLMMessages returns only messages that should be sent to the LLM,
// filtering out UI-only system messages (notifications, command results, etc.)
// and history a compaction summary replaced.
func (m ChatModel) GetLLMMessages() []ChatMessage {
	filtered := make([]ChatMessage, 0, len(m.messages))
	for _, msg := range m.messages {
		if msg.Role != "system" && !isCompacted(msg) {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

func isCompacted(msg ChatMessage) bool {
	c, _ := msg.Metadata["compacted"].(bool)
	return c
}

// ApplySummary replaces the first cut LLM messages with summary (#174). The
// replaced messages stay in the scrollback, marked compacted so they are no
// longer sent; the summary messages are hidden from view and sent in their
// place.
func (m ChatModel) ApplySummary(cut int, summary []ChatMessage) ChatModel {
	msgs := make([]ChatMessage, 0, len(m.messages)+len(summary))
	marked, inserted := 0, false
	for _, msg := range m.messages {
		llmVisible := msg.Role != "system" && !isCompacted(msg)
		if llmVisible && marked < cut {
			meta := make(map[string]any, len(msg.Metadata)+1)
			for k, v := range msg.Metadata {
				meta[k] = v
			}
			meta["compacted"] = true
			msg.Metadata = meta
			msg.ProviderBlocks = nil // replaced by the summary: an edit (2.0 F3)
			marked++
		} else if llmVisible && !inserted {
			msgs = append(msgs, hideAll(summary)...)
			inserted = true
		}
		msgs = append(msgs, msg)
	}
	if !inserted {
		msgs = append(msgs, hideAll(summary)...)
	}
	m.messages = msgs
	return m
}

func hideAll(in []ChatMessage) []ChatMessage {
	out := make([]ChatMessage, len(in))
	for i, msg := range in {
		meta := map[string]any{"hidden": true}
		for k, v := range msg.Metadata {
			meta[k] = v
		}
		msg.Metadata = meta
		out[i] = msg
	}
	return out
}

// RestoreMessages appends saved history as is, keeping tool calls, tool
// results and metadata that the Add* helpers would drop. Answered user
// messages are marked as past their UserPromptSubmit hooks, so resuming
// does not re-check them (MarkAnsweredPromptsHooked).
func (m ChatModel) RestoreMessages(msgs []ChatMessage) ChatModel {
	msgs = append([]ChatMessage(nil), msgs...)
	MarkAnsweredPromptsHooked(msgs)
	m.messages = append(m.messages, msgs...)
	m.updateContent()
	m.viewport.GotoBottom()
	return m
}

// Clear clears all messages and function calls.
func (m ChatModel) Clear() ChatModel {
	m.messages = []ChatMessage{}
	m.functionCalls = []FunctionCall{}
	m.updateContent()
	return m
}

// ToggleSkillCalls toggles the visibility of skill call logs.
// A chat following the conversation stays at the bottom, so the latest
// reply and tool log stay on screen (#351); a scrolled-up one keeps its
// place.
func (m ChatModel) ToggleSkillCalls() ChatModel {
	m.showSkillCalls = !m.showSkillCalls
	m.updateContent()
	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
	return m
}

// ShowingSkillCalls returns whether skill call logs are currently visible.
func (m ChatModel) ShowingSkillCalls() bool {
	return m.showSkillCalls
}

// updateContent rebuilds the viewport content from messages.
func (m *ChatModel) updateContent() {
	if !m.ready {
		return
	}

	var lines []string
	contentWidth := m.width - 4 // Account for padding and some margin

	// Render messages (skip tool results - only LLM needs to see them)
	// Find the index of the last assistant message so we know which one
	// is currently being typed (skip Glamour for it).
	lastAssistantIdx := -1
	if m.typingActive {
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].Role == "assistant" {
				lastAssistantIdx = i
				break
			}
		}
	}

	// With the logs shown, each tool call sits in the turn it ran in:
	// before the first message newer than it, so the latest reply and its
	// tool log end the chat together (#351). Calls newer than every
	// message come last.
	var calls []FunctionCall
	if m.showSkillCalls {
		calls = append(calls, m.functionCalls...)
		sort.SliceStable(calls, func(a, b int) bool { return calls[a].Timestamp.Before(calls[b].Timestamp) })
	}
	next := 0
	emitCalls := func(before time.Time, all bool) {
		start := next
		for next < len(calls) && (all || calls[next].Timestamp.Before(before)) {
			lines = append(lines, m.renderFunctionCall(calls[next], contentWidth))
			next++
		}
		if next > start {
			lines = append(lines, "")
		}
	}

	// A message can be stamped later than the ones after it (a compaction
	// summary is stamped when it is made, ahead of the tail it keeps), so
	// each message flushes calls older than the oldest stamp from it to the
	// end, not its own: it never pulls in calls from the turns after it.
	bound := make([]time.Time, len(m.messages))
	var oldest time.Time
	for i := len(m.messages) - 1; i >= 0; i-- {
		if ts := m.messages[i].Timestamp; !ts.IsZero() && (oldest.IsZero() || ts.Before(oldest)) {
			oldest = ts
		}
		bound[i] = oldest
	}

	for i, msg := range m.messages {
		emitCalls(bound[i], false)
		// Don't render tool results in UI - they're for LLM only
		if msg.Role == "tool" {
			continue
		}
		// A tool-call-only turn has nothing to show; the tool cards cover it.
		if msg.Role == "assistant" && msg.Content == "" && len(msg.ToolCalls) > 0 {
			continue
		}
		// Skip hidden messages (LLM directives like identity changes)
		if msg.Metadata != nil {
			if hidden, ok := msg.Metadata["hidden"].(bool); ok && hidden {
				continue
			}
		}
		skipMarkdown := (i == lastAssistantIdx)
		lines = append(lines, m.renderMessageOpt(msg, contentWidth, skipMarkdown))
		lines = append(lines, "") // Spacing between messages
	}

	emitCalls(time.Time{}, true)

	content := strings.Join(lines, "\n")
	m.viewport.SetContent(content)
}

// renderMessageOpt renders a chat message, optionally skipping Glamour.
// skipMarkdown is set during the typing animation so that ANSI-styled
// corruption glyphs at the cursor don't pass through the markdown
// renderer (which garbles them and causes viewport reflow).
func (m ChatModel) renderMessageOpt(msg ChatMessage, width int, skipMarkdown bool) string {
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

	// Try markdown rendering for assistant messages
	var styledContent string
	if msg.plain {
		// As written, never through markdown; wrapText keeps the indent
		// and column alignment of rows wider than the chat.
		styledContent = MessageRoleStyle(msg.Role).Render(wrapText(msg.Content, width-2))
	} else if !skipMarkdown && (msg.Role == "assistant" || msg.Role == "system") {
		rendered := renderMarkdown(msg.Content, width-2)
		if rendered != msg.Content {
			// Glamour handled it — already styled
			styledContent = rendered
		} else {
			// Fallback: plain text with role-based styling
			contentStyle := MessageRoleStyle(msg.Role)
			styledContent = contentStyle.Render(wrapText(msg.Content, width-2))
		}
	} else {
		// User messages or typing-active: simple wrap with role style
		contentStyle := MessageRoleStyle(msg.Role)
		styledContent = contentStyle.Render(wrapText(msg.Content, width-2))
	}

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

// wrapText wraps text to the specified width. A line that fits is kept as
// written; a longer one wraps with a hanging indent (wrapLine), so command
// help and tables keep their indent and columns (#350).
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) <= width {
			lines[i] = strings.TrimRight(line, " \t")
			continue
		}
		lines[i] = wrapLine(line, width)
	}
	return strings.Join(lines, "\n")
}

// wrapLine wraps one line wider than width. The text up to its hanging
// column stays as written on the first row and the rest wraps beside it;
// continuation rows are indented to that column. The hanging column is the
// start of the rightmost column (text after a run of two or more spaces)
// in the left half of the row (two spaces after a sentence end do not
// count), or else the line's own indent, clamped to half the row: a
// two-column help row hangs under its description, a table row under its
// last column, and prose and pasted code keep their indent.
func wrapLine(line string, width int) string {
	head, tail := splitHang(line, width)
	col := lipgloss.Width(head)
	rows := wrapWords(strings.Fields(tail), width-col)
	if len(rows) == 0 {
		return strings.TrimRight(head, " \t")
	}
	pad := strings.Repeat(" ", col)
	for i := range rows {
		if i == 0 {
			rows[i] = head + rows[i]
		} else {
			rows[i] = pad + rows[i]
		}
	}
	return strings.Join(rows, "\n")
}

// splitHang splits line at its hanging column (see wrapLine): head is kept
// as written, tail is the text that wraps.
func splitHang(line string, width int) (head, tail string) {
	body := strings.TrimLeft(line, " ")
	indent := len(line) - len(body)
	limit := width / 2
	if indent > limit {
		// Deeply indented (pasted code): keep as much indent as leaves
		// half the row for text, on every row.
		return strings.Repeat(" ", limit), body
	}
	cut := indent
	for i := indent; i < len(line); {
		if line[i] != ' ' {
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] == ' ' {
			j++
		}
		// Two spaces after a sentence end are prose, not a column.
		if j-i >= 2 && j < len(line) && !strings.ContainsRune(".!?", rune(line[i-1])) {
			if lipgloss.Width(line[:j]) > limit {
				break
			}
			cut = j
		}
		i = j
	}
	return line[:cut], line[cut:]
}

// wrapWords fills rows of at most width cells with words. A word wider than
// the row (a path from /init or /export) breaks across rows instead of
// being cut at the edge.
func wrapWords(words []string, width int) []string {
	var rows []string
	cur := ""
	for _, word := range words {
		if lipgloss.Width(word) > width {
			if cur != "" {
				rows = append(rows, cur)
			}
			parts := breakWord(word, width)
			rows = append(rows, parts[:len(parts)-1]...)
			cur = parts[len(parts)-1]
			continue
		}
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= width:
			cur += " " + word
		default:
			rows = append(rows, cur)
			cur = word
		}
	}
	if cur != "" {
		rows = append(rows, cur)
	}
	return rows
}

// breakWord splits a word wider than width (it may carry ANSI styling)
// into rows of at most width cells.
func breakWord(word string, width int) []string {
	rows := strings.Split(lipgloss.NewStyle().Width(width).Render(word), "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	return rows
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

// SyncLLM makes the chat's LLM messages match history, a snapshot of a
// running turn's loop history (2.0 F2d). The loop only appends messages and
// rewrites tool results in place (pruning), and its input was this chat's
// GetLLMMessages, so the chat's LLM messages line up with history position
// by position: each takes its history version (content, tool calls,
// metadata) and the rest of history is appended. System lines and
// summarized messages are not LLM messages and keep their places. With
// keepLive, the last LLM message keeps its content if it is an assistant
// reply still being typed out. An empty text-only assistant reply before
// the last LLM message (a resumed session, a turn from before the loop) is
// dropped: the adapter removes those from the loop's input and from every
// snapshot, so keeping it would shift every later position by one. The
// last one is the live typing bubble, which starts empty. A position where
// the two disagree (syncDrift) is logged; the loop's copy still wins.
func (m ChatModel) SyncLLM(history []ChatMessage, keepLive bool) ChatModel {
	last := -1
	for i, msg := range m.messages {
		if msg.Role != "system" && !isCompacted(msg) {
			last = i
		}
	}
	msgs := make([]ChatMessage, 0, len(m.messages)+len(history))
	j := 0
	for i, msg := range m.messages {
		if msg.Role == "system" || isCompacted(msg) || j >= len(history) {
			msgs = append(msgs, msg)
			continue
		}
		if i != last && IsEmptyReply(msg) {
			continue
		}
		h := history[j]
		if drift := syncDrift(msg, h); drift != "" {
			LogInfo(fmt.Sprintf("chat history drift at LLM position %d: %s", j, drift))
		}
		j++
		if keepLive && i == last && msg.Role == "assistant" && h.Role == "assistant" {
			h.Content = msg.Content
		}
		msgs = append(msgs, h)
	}
	msgs = append(msgs, history[j:]...)
	m.messages = msgs
	m.updateContent()
	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
	return m
}

// DropEmptyLastReply removes an empty text-only assistant reply that is the
// last LLM message (a bubble an interrupt left empty). Every turn start
// calls it, so such a stale reply is never the last message when a keepLive
// snapshot arrives: SyncLLM would keep its empty content over the loop's
// reply (Task 9 review ruling).
func (m ChatModel) DropEmptyLastReply() ChatModel {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.Role == "system" || isCompacted(msg) {
			continue
		}
		if !IsEmptyReply(msg) {
			return m
		}
		m.messages = append(append([]ChatMessage(nil), m.messages[:i]...), m.messages[i+1:]...)
		m.updateContent()
		return m
	}
	return m
}

// syncDrift describes how the chat's message and the loop's message at the
// same position disagree, or returns "". SyncLLM takes the loop's copy
// either way; the log is how a broken positional invariant shows up.
func syncDrift(chat, loop ChatMessage) string {
	switch {
	case chat.Role != loop.Role:
		return fmt.Sprintf("role %s in the chat, %s in the loop", chat.Role, loop.Role)
	case chat.ToolCallID != loop.ToolCallID:
		return fmt.Sprintf("tool call %q in the chat, %q in the loop", chat.ToolCallID, loop.ToolCallID)
	case chat.Role == "user" && (chat.Content != loop.Content || !chat.Timestamp.Equal(loop.Timestamp)):
		return fmt.Sprintf("user prompt %q (%s) in the chat, %q (%s) in the loop",
			truncateQueued(chat.Content), chat.Timestamp.Format(time.RFC3339Nano),
			truncateQueued(loop.Content), loop.Timestamp.Format(time.RFC3339Nano))
	}
	return ""
}

// AppendLLM adds one message as the loop recorded it (a steer that joined,
// a Stop hook's continuation), metadata included.
func (m ChatModel) AppendLLM(msg ChatMessage) ChatModel {
	m.messages = append(append([]ChatMessage(nil), m.messages...), msg)
	m.updateContent()
	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
	return m
}
