package loop

import (
	"context"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// A steering re-run keeps the accepted reply's ProviderBlocks and none of
// the dropped reply's (2.0 W3 with F3-2).
func TestSteeringRerunKeepsOnlyTheAcceptedRepliesBlocks(t *testing.T) {
	dropped := mustBlocks(t, `{"type":"thinking","thinking":"drop","signature":"s1"}`, `{"type":"tool_use","id":"c1","name":"bash","input":{}}`)
	accepted := mustBlocks(t, `{"type":"thinking","thinking":"keep","signature":"s2"}`, `{"type":"text","text":"done"}`)
	llmStub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			callsWithBlocks(cb, dropped, [3]string{"c1", "bash", `{"command":"rm -rf /"}`})
			return nil
		}
		cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: "done"})
		cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop", ProviderBlocks: accepted})
		return nil
	}}
	l := &Loop{Client: llmStub, Tools: newRegistry(&fakeTool{name: "bash"}), Limits: DefaultLimits(), Steering: &fakeSteering{onCall: "bash"}}
	msgs, res, err := l.Run(context.Background(), []Message{{Role: "user", Content: "x"}})
	if err != nil || res.FinalText != "done" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	last := msgs[len(msgs)-1]
	assertBlocks(t, last, accepted, "accepted reply")
	for _, m := range msgs[:len(msgs)-1] {
		if m.ProviderBlocks != nil {
			t.Fatalf("the dropped reply's blocks reached the history: %+v", m)
		}
	}
}

// A dropped reply that reported BlocksRejected still strips the replayed
// blocks, so the re-run does not send them again: whether a tool-arguments
// rule or the end-of-stream check dropped it.
func TestSteeringRerunHonoursBlocksRejected(t *testing.T) {
	for _, viaCalls := range []bool{true, false} {
		old := mustBlocks(t, `{"type":"thinking","thinking":"old","signature":"s0"}`, `{"type":"text","text":"earlier"}`)
		earlier := tui.AttachProviderBlocks(Message{Role: "assistant", Content: "earlier"}, old)
		llmStub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
			if n == 0 {
				if viaCalls {
					cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: "c1", ToolName: "bash"})
					cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: "c1", ToolName: "bash", CompleteInput: `{}`})
				} else {
					cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: "Audio saved: x"})
				}
				cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop", BlocksRejected: true})
				return nil
			}
			sayText(cb, "done", nil)
			return nil
		}}
		var st Steering = &fakeSteering{onCall: "bash"}
		if !viaCalls {
			st = &endSteering{}
		}
		l := &Loop{Client: llmStub, Tools: newRegistry(&fakeTool{name: "bash"}), Limits: DefaultLimits(), Steering: st}
		msgs, res, err := l.Run(context.Background(), []Message{{Role: "user", Content: "a"}, earlier, {Role: "user", Content: "b"}})
		if err != nil || res.FinalText != "done" || llmStub.calls != 2 {
			t.Fatalf("viaCalls=%v: res=%+v err=%v calls=%d", viaCalls, res, err, llmStub.calls)
		}
		if _, ok := tui.ReplayBlocks(msgs[1], blocksKey); ok {
			t.Errorf("viaCalls=%v: the rejected blocks are still in the history", viaCalls)
		}
		if _, ok := tui.ReplayBlocks(llmStub.request(1)[1], blocksKey); ok {
			t.Errorf("viaCalls=%v: the re-run sent the rejected blocks again", viaCalls)
		}
	}
}
