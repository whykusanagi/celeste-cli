package tui

// Test support for package main's characterization tests (2.0 F1). Read-only.

// DebugMessages returns the chat history as the model holds it.
func (m AppModel) DebugMessages() []ChatMessage { return m.chat.GetMessages() }

// DebugTurnActive reports whether a turn is running (see turnActive).
func (m AppModel) DebugTurnActive() bool { return m.turnActive() }

// DebugQueued is the number of queued steers (in the app or handed to the
// running loop) plus follow-ups.
func (m AppModel) DebugQueued() int { return len(m.steerQueue) + len(m.followUpQueue) + m.loopSteers }

// DebugInterrupted reports whether the last turn was stopped by Esc (see interrupted).
func (m AppModel) DebugInterrupted() bool { return m.interrupted }

// DebugPermissionPromptActive reports whether the permission modal is up.
func (m AppModel) DebugPermissionPromptActive() bool { return m.permissionPrompt.Active() }

// DebugLLMMessages returns the messages the next turn would send.
func (m AppModel) DebugLLMMessages() []ChatMessage { return m.chat.GetLLMMessages() }
