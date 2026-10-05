package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// turnActive reports whether a turn is still running: a request streaming,
// a reply still being typed out, or tools executing. Input submitted during a
// turn is queued rather than sent as a concurrent request (#172).
func (m AppModel) turnActive() bool {
	return m.turn != nil ||
		m.streaming ||
		m.typingContent != "" ||
		m.cancelFunc != nil ||
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
	switch {
	case followUp:
		m.followUpQueue = append(m.followUpQueue, content)
		kind = "follow-up (sent after this reply)"
	case m.turn != nil && !m.interrupted:
		// The loop joins it at the next tool boundary, checked by
		// UserPromptSubmit then (2.0 F2d: Loop.Steer replaces the queue).
		m.turn.Steer(content)
		m.loopSteers++
	default:
		m.steerQueue = append(m.steerQueue, content) // sent after the turn
	}
	m.chat = m.chat.AddSystemMessage(fmt.Sprintf("⏳ Queued %s: %s", kind, truncateQueued(content)))
	m.status = m.status.SetText(fmt.Sprintf("%d queued · Enter steers · Tab follows up · Esc interrupts",
		len(m.steerQueue)+len(m.followUpQueue)+m.loopSteers))
	return m
}

// dispatchQueued sends the next queued message once the turn has finished:
// leftover steers first (joined into one message), then follow-ups one at a
// time. It returns a nil command when there is nothing to send yet.
func (m AppModel) dispatchQueued() (AppModel, tea.Cmd) {
	if m.dispatchPending || m.handingOff || m.viewMode != "chat" || m.turnActive() ||
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

// interrupt stops the running turn (Esc): the run's context is cancelled,
// so a streaming request stops and tools that honour their context stop
// (every call still gets a result). The reply so far is kept. The turn ends
// when the loop reports it (TurnDoneMsg); steers it never joined come back.
func (m AppModel) interrupt() AppModel {
	if m.turn != nil {
		m.turn.Cancel()
	}
	if m.cancelFunc != nil {
		m.cancelFunc()
		m.cancelFunc = nil
	}
	m.orchRun = 0 // a cancelled /orch run's later events are ignored
	// So are a cancelled /agent run's: it has ended for the chat, and its
	// late terminal event must not end the next run (2.0 F2e).
	m = m.endAgentRun()
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
	if m.turn != nil {
		m.status = m.status.SetText("Interrupting…")
	} else {
		m.status = m.status.SetText("Interrupted")
	}
	m.persistSession()
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
	return fitRow(s, 80)
}
