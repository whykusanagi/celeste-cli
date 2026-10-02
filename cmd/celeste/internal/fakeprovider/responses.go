package fakeprovider

import (
	"io"
	"net/http"
	"strconv"
	"testing"
)

// NewOpenAIResponses serves POST /v1/responses as a streaming OpenAI
// Responses endpoint (2.0 W8) and POST /v1/chat/completions from the same
// script, so a test can follow a fallback from one to the other with one
// server. Pair it with llm.NewResponsesBackend.
func NewOpenAIResponses(t testing.TB, turns ...Turn) *Server {
	s := newServer(t, "/v1", nil, turns)
	s.writers = map[string]func(http.ResponseWriter, Turn){
		"/v1/responses":        writeResponses,
		"/v1/chat/completions": writeOpenAI,
	}
	return s
}

// writeResponses streams turn as Responses API server-sent events: the
// reasoning item, a message item for the text, a function_call item per
// tool call, then the terminal event.
func writeResponses(w http.ResponseWriter, turn Turn) {
	w.Header().Set("Content-Type", "text/event-stream")
	seq := 0
	ev := func(typ string, fields map[string]any) {
		fields["type"] = typ
		fields["sequence_number"] = seq
		seq++
		sse(w, typ, fields)
	}
	response := func(status string, output []any) map[string]any {
		r := map[string]any{"id": "resp_fake", "object": "response", "created_at": 0, "model": "fake-model", "status": status, "output": output}
		if status != "in_progress" {
			r["usage"] = map[string]any{
				"input_tokens": 100, "input_tokens_details": map[string]any{"cached_tokens": 40},
				"output_tokens": 10, "output_tokens_details": map[string]any{"reasoning_tokens": 0},
				"total_tokens": 110,
			}
		}
		return r
	}
	ev("response.created", map[string]any{"response": response("in_progress", []any{})})
	ev("response.in_progress", map[string]any{"response": response("in_progress", []any{})})

	output := []any{}
	item := func(added map[string]any, deltas func(itemID string, index int), done map[string]any) {
		index := len(output)
		ev("response.output_item.added", map[string]any{"output_index": index, "item": added})
		if deltas != nil {
			deltas(added["id"].(string), index)
		}
		ev("response.output_item.done", map[string]any{"output_index": index, "item": done})
		output = append(output, done)
	}
	if r := turn.Reasoning; r != nil {
		item(map[string]any{"id": r.ID, "type": "reasoning", "summary": []any{}}, nil,
			map[string]any{"id": r.ID, "type": "reasoning", "encrypted_content": r.Encrypted,
				"summary": []any{map[string]any{"type": "summary_text", "text": r.Summary}}})
	}
	if turn.Text != "" {
		id := "msg_" + strconv.Itoa(len(output))
		item(map[string]any{"id": id, "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}},
			func(itemID string, index int) {
				ev("response.content_part.added", map[string]any{"item_id": itemID, "output_index": index, "content_index": 0,
					"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
				ev("response.output_text.delta", map[string]any{"item_id": itemID, "output_index": index, "content_index": 0, "delta": turn.Text})
				ev("response.output_text.done", map[string]any{"item_id": itemID, "output_index": index, "content_index": 0, "text": turn.Text})
				ev("response.content_part.done", map[string]any{"item_id": itemID, "output_index": index, "content_index": 0,
					"part": map[string]any{"type": "output_text", "text": turn.Text, "annotations": []any{}}})
			},
			map[string]any{"id": id, "type": "message", "role": "assistant", "status": "completed",
				"content": []any{map[string]any{"type": "output_text", "text": turn.Text, "annotations": []any{}}}})
	}
	for i, tc := range turn.ToolCalls {
		id := "fc_" + strconv.Itoa(i)
		item(map[string]any{"id": id, "type": "function_call", "call_id": tc.ID, "name": tc.Name, "arguments": "", "status": "in_progress"},
			func(itemID string, index int) {
				ev("response.function_call_arguments.delta", map[string]any{"item_id": itemID, "output_index": index, "delta": tc.Args})
				ev("response.function_call_arguments.done", map[string]any{"item_id": itemID, "output_index": index, "arguments": tc.Args})
			},
			map[string]any{"id": id, "type": "function_call", "call_id": tc.ID, "name": tc.Name, "arguments": tc.Args, "status": "completed"})
	}

	code := turn.FailCode
	if code == "" {
		code = "server_error"
	}
	switch {
	case turn.Truncate:
		return
	case turn.Drop:
		dropMidEvent(w)
	case turn.Error != "":
		ev("error", map[string]any{"code": code, "message": turn.Error, "param": nil})
	case turn.Fail != "":
		r := response("failed", output)
		r["error"] = map[string]any{"code": code, "message": turn.Fail}
		ev("response.failed", map[string]any{"response": r})
	case turn.Incomplete != "":
		r := response("incomplete", output)
		r["incomplete_details"] = map[string]any{"reason": turn.Incomplete}
		ev("response.incomplete", map[string]any{"response": r})
	default:
		ev("response.completed", map[string]any{"response": response("completed", output)})
	}
}

// dropMidEvent writes half an event, then closes the connection without
// ending the chunked body, as a connection lost mid-reply does.
func dropMidEvent(w http.ResponseWriter) {
	_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"resp")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	_ = conn.Close()
}
