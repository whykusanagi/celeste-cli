package llm

import (
	"context"
	"errors"
	"testing"
	"time"

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

// thinkOnlyBackend streams n reasoning deltas, one every gap, and nothing
// else; it never touches HTTP, so the only activity the stall watch sees is
// the stream callback. n == 0 sends nothing and waits for the context.
type thinkOnlyBackend struct {
	LLMBackend
	n   int
	gap time.Duration
}

func (b *thinkOnlyBackend) SendMessageStreamEvents(ctx context.Context, _ []tui.ChatMessage, _ []tui.SkillDefinition, cb StreamEventCallback) error {
	if b.n == 0 {
		<-ctx.Done()
		return context.Cause(ctx)
	}
	for i := 0; i < b.n; i++ {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(b.gap):
		}
		cb(StreamEvent{Type: EventThinkingDelta, ThinkingDelta: "hmm"})
	}
	cb(StreamEvent{Type: EventContentDelta, ContentDelta: "done"})
	return nil
}

// A model that only streams reasoning for longer than the stall timeout,
// each delta inside it, is alive: the reasoning callbacks touch the stall
// watch, and every thinking event still reaches the caller.
func TestStreamEventsReasoningOnlyKeepsTheStallTimerAlive(t *testing.T) {
	const stall = 100 * time.Millisecond
	b := &thinkOnlyBackend{n: 12, gap: 30 * time.Millisecond} // ~360ms > stall
	c := NewClientWithBackend(&Config{Timeout: stall}, nil, b)
	thinking, reply := 0, ""
	start := time.Now()
	err := c.SendMessageStreamEvents(context.Background(), nil, nil, func(ev StreamEvent) {
		switch ev.Type {
		case EventThinkingDelta:
			thinking++
		case EventContentDelta:
			reply += ev.ContentDelta
		}
	})
	if err != nil {
		t.Fatalf("err = %v, want nil: reasoning deltas are activity", err)
	}
	if d := time.Since(start); d <= stall {
		t.Fatalf("stream took %v, want longer than the %v stall timeout", d, stall)
	}
	if thinking != 12 || reply != "done" {
		t.Fatalf("thinking=%d reply=%q, want 12 thinking events and the reply", thinking, reply)
	}
}

// A stream that sends nothing at all stalls.
func TestStreamEventsSilentStreamStalls(t *testing.T) {
	c := NewClientWithBackend(&Config{Timeout: 100 * time.Millisecond}, nil, &thinkOnlyBackend{})
	start := time.Now()
	err := c.SendMessageStreamEvents(context.Background(), nil, nil, func(StreamEvent) {})
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("stall took %v to detect, want about the 100ms timeout", d)
	}
}
