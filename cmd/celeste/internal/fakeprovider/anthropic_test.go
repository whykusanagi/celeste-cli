package fakeprovider

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// anthropicEvents posts to the fake's /v1/messages with header set and
// returns the SSE data objects in order.
func anthropicEvents(t *testing.T, srv *Server, header map[string]string) []map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.BaseURL()+"/v1/messages", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []map[string]any
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func TestAnthropicRedactedThinkingTransformationsAndHeaders(t *testing.T) {
	srv := NewAnthropic(t,
		Turn{
			Thinking:         &Thinking{Text: "plan", Signature: "sig-1"},
			RedactedThinking: "opaque",
			Text:             "hi",
			Transformations:  `[{"type":"thinking_dropped","path":"messages.1.content.0","reason":"prefix_binding_mismatch"}]`,
		},
		Turn{Text: "plain"},
	)
	evs := anthropicEvents(t, srv, map[string]string{"anthropic-beta": "thinking-binding-controls-2026-08-01"})
	msg := evs[0]["message"].(map[string]any)
	tr := msg["input_transformations"].([]any)[0].(map[string]any)
	if tr["type"] != "thinking_dropped" || tr["reason"] != "prefix_binding_mismatch" {
		t.Fatalf("input_transformations = %v", msg["input_transformations"])
	}
	var starts []string
	for _, e := range evs {
		if e["type"] == "content_block_start" {
			cb := e["content_block"].(map[string]any)
			starts = append(starts, cb["type"].(string))
			if cb["type"] == "redacted_thinking" && cb["data"] != "opaque" {
				t.Fatalf("redacted block = %v", cb)
			}
		}
	}
	if got := strings.Join(starts, ","); got != "thinking,redacted_thinking,text" {
		t.Fatalf("blocks = %s", got)
	}
	if got := srv.Requests()[0].Header.Get("Anthropic-Beta"); got != "thinking-binding-controls-2026-08-01" {
		t.Fatalf("recorded header = %q", got)
	}

	evs = anthropicEvents(t, srv, nil)
	if _, ok := evs[0]["message"].(map[string]any)["input_transformations"]; ok {
		t.Fatal("a turn without Transformations must not carry the field")
	}
}

func TestAnthropicImageLimitAnswers400LikeTheRealAPI(t *testing.T) {
	srv := NewAnthropic(t, Turn{Text: "ok"})
	AnthropicImageLimit(srv, 5<<20)
	post := func(data string) *http.Response {
		body, err := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "content": []any{
				map[string]any{"type": "image", "source": map[string]any{"type": "base64", "data": data}}}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.Post(srv.BaseURL()+"/v1/messages", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	resp := post(strings.Repeat("A", 6<<20))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var e struct{ Error struct{ Message string } }
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil || e.Error.Message != "image exceeds 5 MB maximum" {
		t.Fatalf("error = %+v, %v", e, err)
	}
	if srv.Remaining() != 1 {
		t.Fatal("a rejected request must not consume a turn")
	}
	if resp := post("QUJD"); resp.StatusCode != http.StatusOK {
		t.Fatalf("small image status = %d", resp.StatusCode)
	}
}
