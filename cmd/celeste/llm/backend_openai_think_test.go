package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// thinkDeltas is a qwen3-style reply from a server without a reasoning
// parser, with both tags split across chunk boundaries.
var thinkDeltas = []string{"<thi", "nk>Okay, the user ", "wants a greeting.</th", "ink>\n\nHi, ", "darling."}

func newThinkBackend(t *testing.T, turns ...fakeprovider.Turn) (*OpenAIBackend, *fakeprovider.Server) {
	t.Helper()
	srv := fakeprovider.NewOpenAI(t, turns...)
	return NewOpenAIBackend(&Config{APIKey: "test", BaseURL: srv.BaseURL(), Model: "qwen3:14b"}), srv
}

func streamEvents(t *testing.T, b *OpenAIBackend, msgs []tui.ChatMessage) []StreamEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var evs []StreamEvent
	if err := b.SendMessageStreamEvents(ctx, msgs, nil, func(ev StreamEvent) { evs = append(evs, ev) }); err != nil {
		t.Fatalf("SendMessageStreamEvents: %v", err)
	}
	return evs
}

func contentOf(evs []StreamEvent) string {
	var sb strings.Builder
	for _, ev := range evs {
		if ev.Type == EventContentDelta {
			sb.WriteString(ev.ContentDelta)
		}
	}
	return sb.String()
}

var hello = []tui.ChatMessage{{Role: "user", Content: "hello"}}

func TestOpenAIStreamEventsStripsThinkBlock(t *testing.T) {
	b, _ := newThinkBackend(t, fakeprovider.Turn{Deltas: thinkDeltas})
	evs := streamEvents(t, b, hello)
	if got := contentOf(evs); got != "Hi, darling." {
		t.Fatalf("reply = %q, want the text after </think> only", got)
	}
	for _, ev := range evs {
		if ev.Type == EventContentDelta && ev.ContentDelta == "" {
			t.Fatalf("empty content delta emitted: %+v", evs)
		}
	}
}

// A stream that ends inside a leading <think> block (cut off, or a tool
// call with no reply text) leaves no reasoning in the reply.
func TestOpenAIStreamEventsDropsUnterminatedLeadingThink(t *testing.T) {
	b, _ := newThinkBackend(t, fakeprovider.Turn{
		Deltas:    []string{"<think>I should read ", "the file first"},
		ToolCalls: []fakeprovider.ToolCall{{ID: "c1", Name: "read_file", Args: `{"path":"a"}`}},
	})
	evs := streamEvents(t, b, hello)
	if got := contentOf(evs); got != "" {
		t.Fatalf("reply = %q, want none", got)
	}
	var done bool
	for _, ev := range evs {
		if ev.Type == EventToolUseDone && ev.ToolName == "read_file" {
			done = true
		}
	}
	if !done {
		t.Fatalf("tool call lost: %+v", evs)
	}
}

func TestOpenAIStreamStripsThinkBlock(t *testing.T) {
	b, _ := newThinkBackend(t, fakeprovider.Turn{Deltas: thinkDeltas})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var sb strings.Builder
	if err := b.SendMessageStream(ctx, hello, nil, func(c StreamChunk) { sb.WriteString(c.Content) }); err != nil {
		t.Fatal(err)
	}
	if sb.String() != "Hi, darling." {
		t.Fatalf("reply = %q", sb.String())
	}
}

func TestOpenAISyncStripsThinkBlock(t *testing.T) {
	b, _ := newThinkBackend(t, fakeprovider.Turn{Deltas: thinkDeltas})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := b.SendMessageSync(ctx, hello, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "Hi, darling." {
		t.Fatalf("content = %q", res.Content)
	}
}

// History saved before the fix (or by another client) still holds the raw
// block; it is never sent back to the model as content.
func TestOpenAIConvertMessagesStripsThinkFromAssistantHistory(t *testing.T) {
	b, srv := newThinkBackend(t, fakeprovider.Turn{Text: "ok"})
	msgs := []tui.ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "<think>Okay, the user wants a greeting.</think>\n\nHi."},
		{Role: "assistant", Content: "<think>need a tool</think>", ToolCalls: []tui.ToolCallInfo{{ID: "c1", Name: "read_file", Arguments: "{}"}}},
		{Role: "tool", Content: "file", ToolCallID: "c1"},
		{Role: "user", Content: "what does <think> mean?"},
	}
	streamEvents(t, b, msgs)
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	raw := string(reqs[0].Raw)
	if strings.Contains(raw, "Okay, the user") || strings.Contains(raw, "need a tool") {
		t.Fatalf("reasoning sent back as content: %s", raw)
	}
	if !strings.Contains(raw, "what does \\u003cthink\\u003e mean?") {
		t.Fatalf("user text altered: %s", raw)
	}
}
