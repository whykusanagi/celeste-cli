package fakeprovider

import (
	"net/http"
	"testing"
)

// NewAnthropic serves POST /v1/messages as a streaming Anthropic endpoint.
// Pair it with llm.Config{Backend: llm.BackendTypeAnthropic}.
func NewAnthropic(t testing.TB, turns ...Turn) *Server {
	return newServer(t, "", writeAnthropic, turns)
}

func writeAnthropic(w http.ResponseWriter, turn Turn) {
	w.Header().Set("Content-Type", "text/event-stream")
	sse(w, "message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_fake", "type": "message", "role": "assistant", "model": "fake-model",
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 100, "output_tokens": 0},
	}})
	idx := 0
	block := func(start map[string]any, deltas ...map[string]any) {
		sse(w, "content_block_start", map[string]any{"type": "content_block_start", "index": idx, "content_block": start})
		for _, d := range deltas {
			sse(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": idx, "delta": d})
		}
		sse(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
		idx++
	}
	if th := turn.Thinking; th != nil {
		block(map[string]any{"type": "thinking", "thinking": "", "signature": ""},
			map[string]any{"type": "thinking_delta", "thinking": th.Text},
			map[string]any{"type": "signature_delta", "signature": th.Signature})
	}
	if turn.Text != "" {
		block(map[string]any{"type": "text", "text": ""}, map[string]any{"type": "text_delta", "text": turn.Text})
	}
	for _, tc := range turn.ToolCalls {
		block(map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": map[string]any{}},
			map[string]any{"type": "input_json_delta", "partial_json": tc.Args})
	}
	stop := "end_turn"
	if len(turn.ToolCalls) > 0 {
		stop = "tool_use"
	}
	sse(w, "message_delta", map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 10}})
	sse(w, "message_stop", map[string]any{"type": "message_stop"})
}
