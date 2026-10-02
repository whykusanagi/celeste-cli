package llm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func TestProviderKey(t *testing.T) {
	a := ProviderKey(BlocksAnthropicMessages, "https://API.anthropic.com/", "claude-x")
	if a != "anthropic-messages|https://api.anthropic.com|claude-x" {
		t.Fatalf("key = %q", a)
	}
	if same := ProviderKey(BlocksAnthropicMessages, " https://api.anthropic.com", "claude-x"); same != a {
		t.Fatalf("the same endpoint spelled differently gave %q, want %q", same, a)
	}
	for _, other := range []string{
		ProviderKey(BlocksAnthropicMessages, "https://api.anthropic.com", "claude-y"), // another model
		ProviderKey(BlocksAnthropicMessages, "https://proxy.example.com", "claude-x"), // another endpoint
		ProviderKey(BlocksOpenAIResponses, "https://api.anthropic.com", "claude-x"),   // another format
	} {
		if other == a {
			t.Fatalf("%q must not equal %q: blocks would replay to a provider that did not issue them", other, a)
		}
	}
}

// blocksBackend answers every path with the same ProviderBlocks.
type blocksBackend struct {
	pb       *tui.ProviderBlocks
	rejected bool
}

func (b blocksBackend) SendMessageStream(_ context.Context, _ []tui.ChatMessage, _ []tui.SkillDefinition, cb StreamCallback) error {
	cb(StreamChunk{Content: "hi", IsFirst: true, IsFinal: true, FinishReason: "stop", ProviderBlocks: b.pb, BlocksRejected: b.rejected})
	return nil
}
func (b blocksBackend) SendMessageStreamEvents(_ context.Context, _ []tui.ChatMessage, _ []tui.SkillDefinition, cb StreamEventCallback) error {
	cb(StreamEvent{Type: EventContentDelta, ContentDelta: "hi"})
	cb(StreamEvent{Type: EventMessageDone, FinishReason: "stop", ProviderBlocks: b.pb, BlocksRejected: b.rejected})
	return nil
}
func (b blocksBackend) SendMessageSync(context.Context, []tui.ChatMessage, []tui.SkillDefinition) (*ChatCompletionResult, error) {
	return &ChatCompletionResult{Content: "hi", ProviderBlocks: b.pb, BlocksRejected: b.rejected}, nil
}
func (b blocksBackend) SetSystemPrompt(string)           {}
func (b blocksBackend) SetThinkingConfig(ThinkingConfig) {}
func (b blocksBackend) Close() error                     { return nil }

// W2 captures blocks on all three Anthropic stream paths; the client must
// hand each one through.
func TestClientForwardsProviderBlocks(t *testing.T) {
	pb, err := tui.NewProviderBlocks(ProviderKey(BlocksAnthropicMessages, "", "m"), []json.RawMessage{json.RawMessage(`{"type":"text","text":"hi"}`)})
	if err != nil {
		t.Fatal(err)
	}
	c := NewClientWithBackend(&Config{Model: "m"}, nil, blocksBackend{pb: pb})
	var fromEvents, fromChunks *tui.ProviderBlocks
	if err := c.SendMessageStreamEvents(context.Background(), nil, nil, func(ev StreamEvent) {
		if ev.Type == EventMessageDone {
			fromEvents = ev.ProviderBlocks
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.SendMessageStream(context.Background(), nil, nil, func(ch StreamChunk) {
		if ch.IsFinal {
			fromChunks = ch.ProviderBlocks
		}
	}); err != nil {
		t.Fatal(err)
	}
	res, err := c.SendMessageSync(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fromEvents != pb || fromChunks != pb || res.ProviderBlocks != pb {
		t.Fatalf("blocks lost: events %p, chunks %p, sync %p, want %p", fromEvents, fromChunks, res.ProviderBlocks, pb)
	}
}

// A backend that had to drop replayed blocks reports it on every path, so
// the loop can strip them from its history (2.0 F3).
func TestClientForwardsBlocksRejected(t *testing.T) {
	c := NewClientWithBackend(&Config{Model: "m"}, nil, blocksBackend{rejected: true})
	var fromEvents, fromChunks bool
	if err := c.SendMessageStreamEvents(context.Background(), nil, nil, func(ev StreamEvent) {
		if ev.Type == EventMessageDone {
			fromEvents = ev.BlocksRejected
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.SendMessageStream(context.Background(), nil, nil, func(ch StreamChunk) {
		if ch.IsFinal {
			fromChunks = ch.BlocksRejected
		}
	}); err != nil {
		t.Fatal(err)
	}
	res, err := c.SendMessageSync(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !fromEvents || !fromChunks || !res.BlocksRejected {
		t.Fatalf("BlocksRejected lost: events %v, chunks %v, sync %v", fromEvents, fromChunks, res.BlocksRejected)
	}
}
