package tui

import (
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
)

// SessionMessagesFromChat converts the chat history into the form sessions
// store (#174). UI-only system messages are dropped; tool calls, tool
// results and the hidden/compacted flags are kept so a resumed session sends
// the model the same history it had.
func SessionMessagesFromChat(msgs []ChatMessage) []config.SessionMessage {
	out := make([]config.SessionMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Role == "system" {
			continue
		}
		sm := config.SessionMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			Timestamp:  msg.Timestamp,
			ToolCallID: msg.ToolCallID,
			Name:       msg.Name,
			Hidden:     metaFlag(msg, "hidden"),
			Compacted:  metaFlag(msg, "compacted"),
		}
		for _, tc := range msg.ToolCalls {
			sm.ToolCalls = append(sm.ToolCalls, config.SessionToolCall{
				ID:               tc.ID,
				Name:             tc.Name,
				Arguments:        tc.Arguments,
				ThoughtSignature: tc.ThoughtSignature,
			})
		}
		out = append(out, sm)
	}
	return out
}

// ChatMessagesFromSession is the inverse of SessionMessagesFromChat. Tool
// calls and results that lost their partner (a session saved mid tool loop,
// or written by an older build) are dropped: every provider rejects a
// history with an unanswered call or an unrequested result.
func ChatMessagesFromSession(msgs []config.SessionMessage) []ChatMessage {
	answered := make(map[string]bool)
	requested := make(map[string]bool)
	for _, sm := range msgs {
		if sm.Role == "tool" && sm.ToolCallID != "" {
			answered[sm.ToolCallID] = true
		}
		for _, tc := range sm.ToolCalls {
			requested[tc.ID] = true
		}
	}

	out := make([]ChatMessage, 0, len(msgs))
	for _, sm := range msgs {
		if sm.Role == "tool" && !requested[sm.ToolCallID] {
			continue
		}
		msg := ChatMessage{
			Role:       sm.Role,
			Content:    sm.Content,
			Timestamp:  sm.Timestamp,
			ToolCallID: sm.ToolCallID,
			Name:       sm.Name,
		}
		for _, tc := range sm.ToolCalls {
			if !answered[tc.ID] {
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCallInfo{
				ID:               tc.ID,
				Name:             tc.Name,
				Arguments:        tc.Arguments,
				ThoughtSignature: tc.ThoughtSignature,
			})
		}
		if msg.Role == "assistant" && msg.Content == "" && len(msg.ToolCalls) == 0 {
			continue // only held calls that were dropped
		}
		if sm.Hidden || sm.Compacted {
			msg.Metadata = map[string]any{}
			if sm.Hidden {
				msg.Metadata["hidden"] = true
			}
			if sm.Compacted {
				msg.Metadata["compacted"] = true
			}
		}
		out = append(out, msg)
	}
	return CapLoadedToolResults(out, ctxmgr.DefaultMaxToolResultBytes)
}

// loadedCutNote goes in the cut marker of a tool result capped on load.
const loadedCutNote = "This older result was cut when the conversation was loaded; the full output is no longer available. " +
	"If you need the missing part, re-run the tool with narrower output (a line range, grep, head or tail)."

// CapLoadedToolResults cuts tool results longer than maxBytes in a history
// loaded from disk (2.0 F3, ruling 9). A history written before 2.0 may hold
// results that were never capped when recorded, and no transport trim cuts
// them any more. There is no spill on load, so the cut
// (ctxmgr.SnipToolResult) keeps the head and tail with a marker that says
// the rest cannot be recalled. The next save stores the cut version.
// Copy-on-write; returns msgs itself when nothing is cut.
func CapLoadedToolResults(msgs []ChatMessage, maxBytes int) []ChatMessage {
	out := msgs
	copied := false
	for i, m := range msgs {
		if m.Role != "tool" || len(m.Content) <= maxBytes {
			continue
		}
		if !copied {
			out = append([]ChatMessage(nil), msgs...)
			copied = true
		}
		out[i].Content = ctxmgr.SnipToolResult(m.Content, maxBytes, loadedCutNote)
		out[i].ProviderBlocks = nil
	}
	return out
}

// markAnsweredPromptsHooked marks every user message that something came
// after as past its UserPromptSubmit hooks: it was sent in an earlier run
// (sessions do not store the mark). Trailing user messages (a prompt kept
// after an interrupt, or steers that never went out) stay unchecked, so the
// next send checks them (2.0 F0).
func markAnsweredPromptsHooked(msgs []ChatMessage) {
	last := len(msgs) - 1
	for last >= 0 && msgs[last].Role == "user" {
		last--
	}
	for i := 0; i < last; i++ {
		if msgs[i].Role != "user" {
			continue
		}
		meta := map[string]any{MetaPromptHookDone: true}
		for k, v := range msgs[i].Metadata {
			meta[k] = v
		}
		msgs[i].Metadata = meta
	}
}

func metaFlag(msg ChatMessage, key string) bool {
	v, _ := msg.Metadata[key].(bool)
	return v
}
