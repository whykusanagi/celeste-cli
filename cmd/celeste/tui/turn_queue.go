package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// turnActive reports whether a turn is still running: a request streaming,
// a reply still being typed out, or tools executing. Input submitted during a
// turn is queued rather than sent as a concurrent request (#172).
func (m AppModel) turnActive() bool {
	return m.streaming ||
		m.typingContent != "" ||
		m.cancelFunc != nil ||
		m.toolBatchActive ||
		m.toolProgress.Executing()
}

// runsDuringTurn lists the commands that act immediately even while a turn is
// running. /agents is needed to inspect or kill a subagent the turn is
// blocked on.
func runsDuringTurn(content string) bool {
	return content == "/agents" || strings.HasPrefix(content, "/agents ")
}

// enqueue queues input submitted during a turn.
func (m AppModel) enqueue(content string, followUp bool) AppModel {
	kind := "steer (joins at the next tool step)"
	if followUp {
		m.followUpQueue = append(m.followUpQueue, content)
		kind = "follow-up (sent after this reply)"
	} else {
		m.steerQueue = append(m.steerQueue, content)
	}
	m.chat = m.chat.AddSystemMessage(fmt.Sprintf("⏳ Queued %s: %s", kind, truncateQueued(content)))
	m.status = m.status.SetText(fmt.Sprintf("%d queued · Enter steers · Tab follows up · Esc interrupts",
		len(m.steerQueue)+len(m.followUpQueue)))
	return m
}

// dispatchQueued sends the next queued message once the turn has finished:
// leftover steers first (joined into one message), then follow-ups one at a
// time. It returns a nil command when there is nothing to send yet.
func (m AppModel) dispatchQueued() (AppModel, tea.Cmd) {
	if m.dispatchPending || m.viewMode != "chat" || m.turnActive() ||
		m.permissionPrompt.Active() || m.askPrompt.Active() {
		return m, nil
	}
	var next string
	switch {
	case len(m.steerQueue) > 0:
		next = strings.Join(m.steerQueue, "\n\n")
		m.steerQueue = nil
	case len(m.followUpQueue) > 0:
		next = m.followUpQueue[0]
		m.followUpQueue = m.followUpQueue[1:]
	default:
		return m, nil
	}
	m.dispatchPending = true
	return m, SendMessage(next)
}

// injectSteers adds queued steers to the conversation as user messages. It is
// called at a tool boundary, after the tool results and before the follow-up
// request, which is the one point where a user message can join a running
// turn without splitting a tool call from its result.
func (m AppModel) injectSteers() AppModel {
	if len(m.steerQueue) == 0 {
		return m
	}
	for _, s := range m.steerQueue {
		m.chat = m.chat.AddUserMessage(s)
		m.recordSessionMessage("user", s)
	}
	m.steerQueue = nil
	m.persistSession()
	return m
}

// interrupt stops the running turn (Esc). A streaming request is cancelled
// and the reply so far is kept. Tools already executing finish, but queued
// tools are skipped and the model is not asked to continue.
func (m AppModel) interrupt() AppModel {
	if m.cancelFunc != nil {
		m.cancelFunc()
		m.cancelFunc = nil
	}
	if m.typingContent != "" {
		m.chat = m.chat.SetTypingActive(false)
		m.chat = m.chat.SetLastAssistantContent(m.typingContent)
		m.recordSessionMessage("assistant", m.typingContent)
		m.typingContent = ""
		m.typingPos = 0
	}
	m.streaming = false
	m.streamDone = false
	m.interruptPending = false
	m.interrupted = true
	m.status = m.status.SetStreaming(false)

	if m.toolProgress.Executing() {
		m.status = m.status.SetText("Interrupting — waiting for the running tool to finish")
	} else {
		if len(m.pendingToolCalls) > 0 {
			m = m.skipPendingToolCalls()
		}
		m.toolBatchActive = false
		m.pendingToolCallID = ""
		m.status = m.status.SetText("Interrupted")
	}
	m.persistSession()
	return m
}

// skipPendingToolCalls records an "interrupted" result for every queued tool
// call, so each assistant tool call still has a result in the history.
func (m AppModel) skipPendingToolCalls() AppModel {
	for _, call := range m.pendingToolCalls {
		m.chat = m.chat.AddToolResult(call.toolCallID, call.name,
			`{"error": true, "message": "not run: interrupted by the user"}`)
		m.toolProgress, _ = m.toolProgress.Update(ToolProgressMsg{
			ToolCallID: call.toolCallID,
			ToolName:   call.name,
			State:      "failed",
		})
	}
	m.pendingToolCalls = nil
	return m
}

// recordSessionMessage appends a message to the current session.
func (m *AppModel) recordSessionMessage(role, content string) {
	if m.currentSession == nil {
		return
	}
	if configSession, ok := m.currentSession.(*config.Session); ok {
		configSession.Messages = append(configSession.Messages, config.SessionMessage{
			Role:      role,
			Content:   content,
			Timestamp: time.Now(),
		})
	}
}

func truncateQueued(s string) string {
	const max = 80
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}
