package fakeprovider

import (
	"io"
	"net/http"
	"testing"
)

// NewOpenAI serves POST /v1/chat/completions as an OpenAI-compatible
// streaming endpoint. The OpenAI backend always streams.
func NewOpenAI(t testing.TB, turns ...Turn) *Server {
	return newServer(t, "/v1", writeOpenAI, turns)
}

func writeOpenAI(w http.ResponseWriter, turn Turn) {
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := func(delta map[string]any, finish any) {
		sse(w, "", map[string]any{
			"id": "chatcmpl-fake", "object": "chat.completion.chunk", "created": 0, "model": "fake-model",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		})
	}
	chunk(map[string]any{"role": "assistant", "content": ""}, nil)
	field := turn.ReasoningField
	if field == "" {
		field = "reasoning"
	}
	for _, d := range turn.ReasoningDeltas {
		chunk(map[string]any{field: d}, nil)
	}
	if len(turn.Deltas) > 0 {
		for _, d := range turn.Deltas {
			chunk(map[string]any{"content": d}, nil)
		}
	} else if turn.Text != "" {
		chunk(map[string]any{"content": turn.Text}, nil)
	}
	for i, tc := range turn.ToolCalls {
		chunk(map[string]any{"tool_calls": []any{map[string]any{
			"index": i, "id": tc.ID, "type": "function",
			"function": map[string]any{"name": tc.Name, "arguments": tc.Args},
		}}}, nil)
	}
	finish := "stop"
	if len(turn.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	chunk(map[string]any{}, finish)
	sse(w, "", map[string]any{
		"id": "chatcmpl-fake", "object": "chat.completion.chunk", "created": 0, "model": "fake-model",
		"choices": []any{}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110,
			"prompt_tokens_details": map[string]any{"cached_tokens": 40}},
	})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}
