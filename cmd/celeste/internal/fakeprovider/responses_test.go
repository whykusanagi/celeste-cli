package fakeprovider

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// events posts body to url and returns the SSE events' data objects, in order.
func events(t *testing.T, url, body string) []map[string]any {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	var out []map[string]any
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("bad event %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

func types(evs []map[string]any) string {
	var ts []string
	for _, e := range evs {
		ts = append(ts, e["type"].(string))
	}
	return strings.Join(ts, ",")
}

func TestResponsesStreamsReasoningTextAndCalls(t *testing.T) {
	srv := NewOpenAIResponses(t, Turn{
		Reasoning: &Reasoning{ID: "rs_1", Summary: "plan", Encrypted: "enc-1"},
		Text:      "hi",
		ToolCalls: []ToolCall{{ID: "call_a", Name: "read_file", Args: `{"path":"a.go"}`}},
	})
	evs := events(t, srv.BaseURL()+"/responses", `{"model":"m","input":[]}`)
	want := "response.created,response.in_progress," +
		"response.output_item.added,response.output_item.done," +
		"response.output_item.added,response.content_part.added,response.output_text.delta,response.output_text.done,response.content_part.done,response.output_item.done," +
		"response.output_item.added,response.function_call_arguments.delta,response.function_call_arguments.done,response.output_item.done," +
		"response.completed"
	if got := types(evs); got != want {
		t.Fatalf("events = %s\nwant     %s", got, want)
	}
	done := evs[len(evs)-1]["response"].(map[string]any)
	out := done["output"].([]any)
	if len(out) != 3 {
		t.Fatalf("output = %v", out)
	}
	rs := out[0].(map[string]any)
	if rs["id"] != "rs_1" || rs["type"] != "reasoning" || rs["encrypted_content"] != "enc-1" {
		t.Fatalf("reasoning item = %v", rs)
	}
	fc := out[2].(map[string]any)
	if fc["id"] != "fc_0" || fc["call_id"] != "call_a" || fc["arguments"] != `{"path":"a.go"}` {
		t.Fatalf("function_call item = %v", fc)
	}
	usage := done["usage"].(map[string]any)
	if usage["input_tokens"] != float64(100) || usage["input_tokens_details"].(map[string]any)["cached_tokens"] != float64(40) {
		t.Fatalf("usage = %v", usage)
	}
	if got := srv.Requests()[0]; got.Path != "/v1/responses" || string(got.Raw) != `{"model":"m","input":[]}` {
		t.Fatalf("request = %+v (raw %s)", got, got.Raw)
	}
}

func TestResponsesTerminalVariants(t *testing.T) {
	srv := NewOpenAIResponses(t,
		Turn{Text: "cut", Incomplete: "max_output_tokens"},
		Turn{Text: "x", Fail: "boom"},
		Turn{Text: "x", Truncate: true},
	)
	inc := events(t, srv.BaseURL()+"/responses", `{}`)
	last := inc[len(inc)-1]
	if last["type"] != "response.incomplete" || last["response"].(map[string]any)["incomplete_details"].(map[string]any)["reason"] != "max_output_tokens" {
		t.Fatalf("incomplete = %v", last)
	}
	failed := events(t, srv.BaseURL()+"/responses", `{}`)
	last = failed[len(failed)-1]
	if last["type"] != "response.failed" || last["response"].(map[string]any)["error"].(map[string]any)["message"] != "boom" {
		t.Fatalf("failed = %v", last)
	}
	cut := events(t, srv.BaseURL()+"/responses", `{}`)
	for _, e := range cut {
		switch e["type"] {
		case "response.completed", "response.incomplete", "response.failed":
			t.Fatalf("truncated stream carried a terminal event: %v", e["type"])
		}
	}
}

// One script serves both transports, so a fallback test needs one server.
func TestResponsesServerAlsoServesChatCompletions(t *testing.T) {
	srv := NewOpenAIResponses(t, Turn{Status: 404, Body: `{"error":{"message":"Not found","type":"invalid_request_error"}}`}, Turn{Text: "from chat"})
	resp, err := http.Post(srv.BaseURL()+"/responses", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want the scripted 404", resp.StatusCode)
	}
	evs := events(t, srv.BaseURL()+"/chat/completions", `{}`)
	var text string
	for _, e := range evs {
		for _, c := range e["choices"].([]any) {
			if s, ok := c.(map[string]any)["delta"].(map[string]any)["content"].(string); ok {
				text += s
			}
		}
	}
	if text != "from chat" {
		t.Fatalf("chat text = %q", text)
	}
}

func TestServerHandleAndUnknownRoutes(t *testing.T) {
	srv := NewOpenAIResponses(t, Turn{Text: "kept"})
	srv.Handle("/v1/responses/compact", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	resp, err := http.Post(srv.BaseURL()+"/responses/compact", "application/json", strings.NewReader(`{"input":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("compact status = %d", resp.StatusCode)
	}
	resp, err = http.Post(srv.BaseURL()+"/embeddings", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown route status = %d", resp.StatusCode)
	}
	if srv.Remaining() != 1 {
		t.Fatalf("remaining = %d: a fixed or unknown route consumed a scripted turn", srv.Remaining())
	}
	if n := len(srv.Requests()); n != 2 {
		t.Fatalf("requests recorded = %d, want 2", n)
	}
}

// An error event ends the stream with the scripted code and message;
// FailCode overrides response.failed's default server_error.
func TestResponsesErrorEventAndFailCode(t *testing.T) {
	srv := NewOpenAIResponses(t,
		Turn{Error: "slow down", FailCode: "rate_limit_exceeded"},
		Turn{Fail: "bad prompt", FailCode: "invalid_prompt"},
	)
	evs := events(t, srv.BaseURL()+"/responses", `{}`)
	last := evs[len(evs)-1]
	if last["type"] != "error" || last["code"] != "rate_limit_exceeded" || last["message"] != "slow down" {
		t.Fatalf("error event = %v", last)
	}
	evs = events(t, srv.BaseURL()+"/responses", `{}`)
	last = evs[len(evs)-1]
	if code := last["response"].(map[string]any)["error"].(map[string]any)["code"]; code != "invalid_prompt" {
		t.Fatalf("failed code = %v", code)
	}
}

// Drop closes the connection in the middle of an event, without ending the
// chunked body: the client's read fails with io.ErrUnexpectedEOF.
func TestResponsesDropCutsMidEvent(t *testing.T) {
	srv := NewOpenAIResponses(t, Turn{Text: "partial", Drop: true})
	resp, err := http.Post(srv.BaseURL()+"/responses", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read err = %v, want io.ErrUnexpectedEOF", err)
	}
	if !strings.Contains(string(b), "response.output_text.delta") || strings.Contains(string(b), "response.completed") {
		t.Fatalf("body = %s", b)
	}
}
