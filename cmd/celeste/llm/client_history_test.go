package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// historyBackend records what each attempt sent and fails the first `fail`
// attempts with a retryable server error.
type historyBackend struct {
	mu   sync.Mutex
	fail int
	seen [][]tui.ChatMessage
}

func (b *historyBackend) attempt(m []tui.ChatMessage) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen = append(b.seen, m)
	if len(b.seen) <= b.fail {
		return errors.New("status code 503")
	}
	return nil
}

func (b *historyBackend) SendMessageStream(_ context.Context, m []tui.ChatMessage, _ []tui.SkillDefinition, cb StreamCallback) error {
	if err := b.attempt(m); err != nil {
		return err
	}
	cb(StreamChunk{Content: "ok", IsFirst: true, IsFinal: true, FinishReason: "stop"})
	return nil
}

func (b *historyBackend) SendMessageStreamEvents(_ context.Context, m []tui.ChatMessage, _ []tui.SkillDefinition, cb StreamEventCallback) error {
	if err := b.attempt(m); err != nil {
		return err
	}
	cb(StreamEvent{Type: EventContentDelta, ContentDelta: "ok"})
	cb(StreamEvent{Type: EventMessageDone, FinishReason: "stop"})
	return nil
}

func (b *historyBackend) SendMessageSync(_ context.Context, m []tui.ChatMessage, _ []tui.SkillDefinition) (*ChatCompletionResult, error) {
	if err := b.attempt(m); err != nil {
		return nil, err
	}
	return &ChatCompletionResult{Content: "ok"}, nil
}

func (b *historyBackend) SetSystemPrompt(string)           {}
func (b *historyBackend) SetThinkingConfig(ThinkingConfig) {}
func (b *historyBackend) Close() error                     { return nil }

// 120000 bytes (~117 KiB): over the deleted 64 KiB transport trim, under the
// loop's 128 KiB record-time cap, so only a transport rewrite could change it.
var bigToolResult = strings.Repeat("line of tool output\n", 6000)

func historyWithBigResult() []tui.ChatMessage {
	return []tui.ChatMessage{
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "c1", Name: "read_file", Arguments: `{}`}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: bigToolResult},
	}
}

func assertSentUnchanged(t *testing.T, be *historyBackend, attempts int) {
	t.Helper()
	if len(be.seen) != attempts {
		t.Fatalf("attempts = %d, want %d", len(be.seen), attempts)
	}
	for i, sent := range be.seen {
		if got := sent[2].Content; got != bigToolResult {
			t.Fatalf("attempt %d sent %d bytes of the tool result, want all %d unchanged (2.0 F3: no transport trim)", i, len(got), len(bigToolResult))
		}
	}
}

// The client sends the history it is given, on the first attempt and on a
// retry (2.0 F3: trimHook and the halving on retry are gone).
func TestClientSendsHistoryUnchanged(t *testing.T) {
	cfg := &Config{Model: "m", Timeout: 5 * time.Second}

	t.Run("stream events, with a retry", func(t *testing.T) {
		be := &historyBackend{fail: 1}
		c := NewClientWithBackend(cfg, nil, be)
		if err := c.SendMessageStreamEvents(context.Background(), historyWithBigResult(), nil, func(StreamEvent) {}); err != nil {
			t.Fatal(err)
		}
		assertSentUnchanged(t, be, 2)
	})
	t.Run("stream", func(t *testing.T) {
		be := &historyBackend{}
		c := NewClientWithBackend(cfg, nil, be)
		if err := c.SendMessageStream(context.Background(), historyWithBigResult(), nil, func(StreamChunk) {}); err != nil {
			t.Fatal(err)
		}
		assertSentUnchanged(t, be, 1)
	})
	t.Run("sync", func(t *testing.T) {
		be := &historyBackend{}
		c := NewClientWithBackend(cfg, nil, be)
		if _, err := c.SendMessageSync(context.Background(), historyWithBigResult(), nil); err != nil {
			t.Fatal(err)
		}
		assertSentUnchanged(t, be, 1)
	})
}
