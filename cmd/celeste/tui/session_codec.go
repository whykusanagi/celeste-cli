package tui

import "github.com/whykusanagi/celeste-cli/cmd/celeste/config"

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
	return out
}

func metaFlag(msg ChatMessage, key string) bool {
	v, _ := msg.Metadata[key].(bool)
	return v
}
