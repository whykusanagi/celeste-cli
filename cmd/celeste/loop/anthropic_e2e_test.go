package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// anthropicRequest is request n's thinking parameter and each assistant
// message's content blocks, as the bytes on the wire.
func anthropicRequest(t *testing.T, srv *fakeprovider.Server, n int) (thinking string, assistants [][]string) {
	t.Helper()
	var body struct {
		Thinking json.RawMessage `json:"thinking"`
		Messages []struct {
			Role    string            `json:"role"`
			Content []json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(srv.Requests()[n].Raw, &body); err != nil {
		t.Fatal(err)
	}
	for _, m := range body.Messages {
		if m.Role != "assistant" {
			continue
		}
		var blocks []string
		for _, c := range m.Content {
			blocks = append(blocks, string(c))
		}
		assistants = append(assistants, blocks)
	}
	return string(body.Thinking), assistants
}

func blocksOf(t *testing.T, m Message, key string) []string {
	t.Helper()
	raws, ok := tui.ReplayBlocks(m, key)
	if !ok {
		t.Fatalf("assistant message has no replayable blocks: %+v", m)
	}
	out := make([]string, len(raws))
	for i, r := range raws {
		out[i] = string(r)
	}
	return out
}

func sameBlocks(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%s:\n got %v\nwant %v", what, got, want)
	}
}

// Spec §5 W2: replay order and signatures are byte-identical across a
// 3-call tool loop and survive a save/resume round trip; a budget-thinking
// model keeps thinking on every continuation because each one replays.
func TestAnthropicThinkingReplaysAcrossAToolLoopAndResume(t *testing.T) {
	hermetic(t)
	step := func(n int) fakeprovider.Turn {
		turn := fakeprovider.Turn{
			Thinking:  &fakeprovider.Thinking{Text: fmt.Sprintf("plan %d", n), Signature: fmt.Sprintf("sig-%d", n)},
			ToolCalls: []fakeprovider.ToolCall{{ID: fmt.Sprintf("toolu_%d", n), Name: "echo", Args: fmt.Sprintf(`{"k":"%d"}`, n)}},
		}
		if n == 2 {
			turn.RedactedThinking = "opaque-2"
		}
		return turn
	}
	srv := fakeprovider.NewAnthropic(t, step(1), step(2), step(3),
		fakeprovider.Turn{Thinking: &fakeprovider.Thinking{Text: "wrap up", Signature: "sig-4"}, Text: "all done"},
		fakeprovider.Turn{Thinking: &fakeprovider.Thinking{Text: "again", Signature: "sig-5"}, Text: "resumed"},
	)
	reg := newRegistry(&fakeTool{name: "echo", safe: true, readOnly: true})
	cfg := &llm.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "claude-haiku-4-5", Timeout: 10 * time.Second, Backend: llm.BackendTypeAnthropic}
	key := llm.ProviderKey(llm.BlocksAnthropicMessages, srv.BaseURL(), "claude-haiku-4-5")
	newLoop := func() *Loop {
		c := llm.NewClient(cfg, reg)
		c.SetThinkingConfig(llm.ThinkingConfig{Enabled: true, Level: "high"})
		return &Loop{Client: c, Tools: reg, Limits: DefaultLimits(), SpillDir: t.TempDir()}
	}

	msgs, res, err := newLoop().Run(context.Background(), userMsg("go"))
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalText != "all done" || res.ToolCalls != 3 {
		t.Fatalf("res = %+v", res)
	}
	var captured [][]string
	for _, m := range msgs {
		if m.Role == "assistant" {
			captured = append(captured, blocksOf(t, m, key))
		}
	}
	if len(captured) != 4 {
		t.Fatalf("captured %d assistant replies, want 4", len(captured))
	}
	if len(captured[1]) != 3 || !strings.Contains(captured[1][1], `"redacted_thinking"`) {
		t.Fatalf("reply 2 blocks = %v, want thinking, redacted_thinking, tool_use in order", captured[1])
	}

	for n := 1; n <= 3; n++ {
		thinking, assistants := anthropicRequest(t, srv, n)
		if !strings.Contains(thinking, "budget_tokens") {
			t.Fatalf("request %d: thinking = %s, want budget thinking on the continuation", n, thinking)
		}
		if len(assistants) != n {
			t.Fatalf("request %d sent %d assistant messages, want %d", n, len(assistants), n)
		}
		for i := range assistants {
			sameBlocks(t, fmt.Sprintf("request %d, assistant %d", n, i), assistants[i], captured[i])
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
	_, res, err = newLoop().Run(context.Background(), append(resumed, Message{Role: "user", Content: "again"}))
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalText != "resumed" {
		t.Fatalf("resumed res = %+v", res)
	}
	_, assistants := anthropicRequest(t, srv, 4)
	if len(assistants) != 4 {
		t.Fatalf("after resume the request sent %d assistant messages, want 4", len(assistants))
	}
	for i := range assistants {
		sameBlocks(t, fmt.Sprintf("after resume, assistant %d", i), assistants[i], captured[i])
	}
}
