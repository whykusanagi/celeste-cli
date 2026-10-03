package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// reasoningItems returns the reasoning items request n sent, in order, as
// the bytes on the wire.
func reasoningItems(t *testing.T, srv *fakeprovider.Server, n int) []string {
	t.Helper()
	var body struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(srv.Requests()[n].Raw, &body); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range body.Input {
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(it, &head) == nil && head.Type == "reasoning" {
			out = append(out, string(it))
		}
	}
	return out
}

// 2.0 W8 through the real loop: every reply's reasoning item is recorded on
// its assistant message and reaches every later request byte for byte,
// including after the session is saved and resumed.
func TestResponsesToolLoopReplaysReasoningAcrossTurnsAndResume(t *testing.T) {
	hermetic(t)
	step := func(n int) fakeprovider.Turn {
		return fakeprovider.Turn{
			Reasoning: &fakeprovider.Reasoning{ID: fmt.Sprintf("rs_%d", n), Summary: fmt.Sprintf("step %d", n), Encrypted: fmt.Sprintf("enc-%d", n)},
			ToolCalls: []fakeprovider.ToolCall{{ID: fmt.Sprintf("call_%d", n), Name: "echo", Args: fmt.Sprintf(`{"k":"%d"}`, n)}},
		}
	}
	srv := fakeprovider.NewOpenAIResponses(t, step(1), step(2), step(3),
		fakeprovider.Turn{Text: "all done"}, fakeprovider.Turn{Text: "resumed"})
	reg := newRegistry(&fakeTool{name: "echo", safe: true, readOnly: true})
	cfg := &llm.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "gpt-test", Timeout: 10 * time.Second, Backend: llm.BackendTypeOpenAIResponses}
	key := llm.ProviderKey(llm.BlocksOpenAIResponses, srv.BaseURL(), "gpt-test")

	l := &Loop{Client: llm.NewClient(cfg, reg), Tools: reg, Limits: DefaultLimits(), SpillDir: t.TempDir()}
	msgs, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalText != "all done" || res.ToolCalls != 3 {
		t.Fatalf("res = %+v", res)
	}

	var captured []string
	for _, m := range msgs {
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		if want := fmt.Sprintf("call_%d", len(captured)+1); m.ToolCalls[0].ID != want {
			t.Fatalf("tool call id = %q, want the call_id %q", m.ToolCalls[0].ID, want)
		}
		raws, ok := tui.ReplayBlocks(m, key)
		if !ok {
			t.Fatalf("assistant message has no replayable blocks: %+v", m)
		}
		captured = append(captured, string(raws[0]))
	}
	if len(captured) != 3 {
		t.Fatalf("captured %d tool-calling replies, want 3", len(captured))
	}
	for n := 1; n <= 3; n++ {
		got := reasoningItems(t, srv, n)
		if len(got) != n {
			t.Fatalf("request %d sent %d reasoning items, want %d", n, len(got), n)
		}
		for i := range got {
			if got[i] != captured[i] {
				t.Fatalf("request %d, item %d:\n got %s\nwant %s", n, i, got[i], captured[i])
			}
		}
	}

	data, err := json.MarshalIndent(tui.SessionMessagesFromChat(msgs), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var saved []config.SessionMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	resumed := tui.ChatMessagesFromSession(saved)
	l2 := &Loop{Client: llm.NewClient(cfg, reg), Tools: reg, Limits: DefaultLimits(), SpillDir: t.TempDir()}
	_, res, err = l2.Run(context.Background(), append(resumed, Message{Role: "user", Content: "again"}))
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalText != "resumed" {
		t.Fatalf("resumed res = %+v", res)
	}
	got := reasoningItems(t, srv, 4)
	if len(got) != 3 {
		t.Fatalf("after resume the request sent %d reasoning items, want 3", len(got))
	}
	for i := range got {
		if got[i] != captured[i] {
			t.Fatalf("after resume, item %d:\n got %s\nwant %s", i, got[i], captured[i])
		}
	}
}
