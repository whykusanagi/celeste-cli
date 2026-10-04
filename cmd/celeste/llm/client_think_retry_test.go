package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// dropAfterThinking fails its first stream with a connection drop after
// only reasoning has arrived, then replies normally.
type dropAfterThinking struct {
	LLMBackend
	calls int
}

func (b *dropAfterThinking) SendMessageStreamEvents(_ context.Context, _ []tui.ChatMessage, _ []tui.SkillDefinition, cb StreamEventCallback) error {
	b.calls++
	cb(StreamEvent{Type: EventThinkingDelta, ThinkingDelta: "let me see"})
	if b.calls == 1 {
		return errors.New("read: connection reset by peer")
	}
	cb(StreamEvent{Type: EventContentDelta, ContentDelta: "Hi"})
	return nil
}

// A drop while the model is still only reasoning is retried: nothing has
// reached the reply yet (L4).
func TestStreamEventsRetriesADropDuringThinkingOnly(t *testing.T) {
	b := &dropAfterThinking{}
	c := NewClientWithBackend(&Config{}, nil, b)
	var reply string
	err := c.SendMessageStreamEvents(context.Background(), nil, nil, func(ev StreamEvent) {
		if ev.Type == EventContentDelta {
			reply += ev.ContentDelta
		}
	})
	if err != nil || b.calls != 2 || reply != "Hi" {
		t.Fatalf("err=%v calls=%d reply=%q, want a retry that replies Hi", err, b.calls, reply)
	}
}
