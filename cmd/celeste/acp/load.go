package acp

import (
	"context"
	"encoding/json"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// loadSession answers session/load (ruling 11): the celeste session with
// that ID is loaded, its Env rebuilt for the editor's cwd, and its
// conversation replayed as session updates before the answer. A session
// this agent already has open is replayed from memory, keeping its Env.
func (a *Agent) loadSession(ctx context.Context, p LoadSessionParams) *RPCError {
	cwd, rerr := checkCwd(p.Cwd)
	if rerr != nil {
		return rerr
	}
	if s := a.session(p.SessionID); s != nil {
		return a.replayOpen(s, p)
	}
	store, err := a.loadStore(p.SessionID)
	if err != nil {
		return &RPCError{Code: CodeInvalidParams, Message: "unknown session " + p.SessionID}
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return &RPCError{Code: CodeInternal, Message: "loading celeste's config: " + err.Error()}
	}
	history := tui.ChatMessagesFromSession(store.GetMessages())
	// Prompts answered in an earlier run passed their UserPromptSubmit
	// hooks then; the next prompt must not re-run them (F0, as the TUI's
	// resume does).
	tui.MarkAnsweredPromptsHooked(history)
	s := &session{id: store.ID, cwd: cwd, cfg: cfg, store: store, allow: map[string]bool{}, history: history}
	if rerr := a.setupEnv(ctx, s, p.McpServers); rerr != nil {
		return rerr
	}
	store.SetWorkspace(cwd)
	if err := a.saveStore(store); err != nil {
		a.logf("acp: session %s: saving the loaded session: %v", s.id, err)
	}
	if !a.addSession(s) {
		s.close()
		// A concurrent load of the same session won, or the agent closed.
		if open := a.session(p.SessionID); open != nil {
			return a.replayOpen(open, p)
		}
		return &RPCError{Code: CodeInternal, Message: "the agent is shutting down"}
	}
	s.replay(a, history)
	return nil
}

// replayOpen replays a session this agent has open. Its Env stays: the
// editor's MCP servers were connected at session/new.
func (a *Agent) replayOpen(s *session, p LoadSessionParams) *RPCError {
	if s.running.Load() {
		return &RPCError{Code: CodeBusy, Message: "a prompt is already running in this session"}
	}
	if len(p.McpServers) > 0 {
		a.logf("acp: session %s is already open; the MCP servers sent with session/load are ignored", s.id)
	}
	s.mu.Lock()
	history := append([]tui.ChatMessage(nil), s.history...)
	s.mu.Unlock()
	s.replay(a, history)
	return nil
}

// replay sends the conversation to the editor (ruling 11): user prompts as
// user_message_chunk, assistant text as agent_message_chunk, and each tool
// call with its result as one tool_call, completed or failed. Messages the
// chat hides (summaries, the app's directives) are not shown.
func (s *session) replay(a *Agent, msgs []tui.ChatMessage) {
	results := map[string]tui.ChatMessage{}
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID != "" {
			results[m.ToolCallID] = m
		}
	}
	for _, m := range msgs {
		if hidden, _ := m.Metadata["hidden"].(bool); hidden {
			continue
		}
		switch m.Role {
		case "user":
			if m.Content != "" {
				s.update(a, UserMessageChunk(m.Content))
			}
		case "assistant":
			if m.Content != "" {
				s.update(a, AgentMessageChunk(m.Content))
			}
			for _, tc := range m.ToolCalls {
				s.update(a, s.replayedCall(tc, results))
			}
		}
	}
}

// replayedCall is a stored tool call and its result as a tool_call update.
func (s *session) replayedCall(tc tui.ToolCallInfo, results map[string]tui.ChatMessage) ToolCall {
	var input map[string]any
	if json.Unmarshal([]byte(tc.Arguments), &input) != nil {
		input = nil
	}
	call := ToolCall{
		SessionUpdate: UpdateToolCall,
		ToolCallID:    tc.ID,
		Title:         toolTitle(tc.Name, input),
		Kind:          toolKind(tc.Name),
		Status:        ToolStatusCompleted,
		Locations:     toolLocations(input, s.cwd),
	}
	if input != nil {
		call.RawInput = input
	}
	if r, ok := results[tc.ID]; ok {
		if failedResult(r.Content) {
			call.Status = ToolStatusFailed
		}
		call.Content = []ToolCallContent{TextToolContent(capText(r.Content, maxToolContent))}
	}
	return call
}

// failedResult reports a tool result the loop stored as an error: its
// error envelope, {"error": true, "tool": …, "message": …}.
func failedResult(content string) bool {
	var env struct {
		Error   bool   `json:"error"`
		Tool    string `json:"tool"`
		Message string `json:"message"`
	}
	return json.Unmarshal([]byte(content), &env) == nil && env.Error && env.Tool != ""
}
