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
		return a.replayOpen(ctx, s, cwd, p)
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
	s := &session{id: store.ID, cwd: cwd, cfg: cfg, store: store, allow: map[string]bool{}, history: history, mcpServers: p.McpServers}
	if rerr := a.setupEnv(ctx, s); rerr != nil {
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
			return a.replayOpen(ctx, open, cwd, p)
		}
		return &RPCError{Code: CodeInternal, Message: "the agent is shutting down"}
	}
	s.replay(a, history)
	return nil
}

// replayOpen replays a session this agent has open. Its Env is rebuilt
// when the editor sends another cwd or MCP servers of its own; otherwise it
// stays. The session is held like a running prompt meanwhile, so no prompt
// interleaves with the replay.
func (a *Agent) replayOpen(ctx context.Context, s *session, cwd string, p LoadSessionParams) *RPCError {
	if !a.hold() {
		return &RPCError{Code: CodeInternal, Message: "the agent is shutting down"}
	}
	defer a.prompts.Done()
	if !s.running.CompareAndSwap(false, true) {
		return &RPCError{Code: CodeBusy, Message: "a prompt is already running in this session"}
	}
	defer s.running.Store(false)
	if cwd != s.cwd || len(p.McpServers) > 0 {
		oldCwd, oldServers := s.cwd, s.mcpServers
		s.cwd, s.mcpServers = cwd, p.McpServers
		if rerr := s.rebuildEnv(ctx, a); rerr != nil {
			s.cwd, s.mcpServers = oldCwd, oldServers
			return rerr
		}
		s.store.SetWorkspace(cwd)
		if err := a.saveStore(s.store); err != nil {
			a.logf("acp: session %s: saving the loaded session: %v", s.id, err)
		}
	}
	s.mu.Lock()
	history := append([]tui.ChatMessage(nil), s.history...)
	s.mu.Unlock()
	s.replay(a, history)
	return nil
}

// replay sends the conversation to the editor (ruling 11): user prompts as
// user_message_chunk, assistant text as agent_message_chunk, and each tool
// call with its result as one tool_call, completed or failed. A call's
// result is looked up among the tool messages right after it, so a call ID
// a provider reused in another turn never borrows that turn's result.
// Messages the chat hides (summaries, the app's directives) are not shown.
func (s *session) replay(a *Agent, msgs []tui.ChatMessage) {
	for i, m := range msgs {
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
			results := map[string]tui.ChatMessage{}
			for j := i + 1; j < len(msgs) && msgs[j].Role == "tool"; j++ {
				if _, seen := results[msgs[j].ToolCallID]; !seen {
					results[msgs[j].ToolCallID] = msgs[j]
				}
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
		Status:        ToolStatusFailed, // no saved result: it never finished
		Locations:     toolLocations(input, s.cwd),
	}
	if input != nil {
		call.RawInput = input
	}
	if r, ok := results[tc.ID]; ok {
		if !failedResult(r.Content) {
			call.Status = ToolStatusCompleted
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
