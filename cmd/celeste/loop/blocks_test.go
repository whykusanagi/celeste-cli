package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

const blocksKey = "test-format||fake-model"

func mustBlocks(t *testing.T, raws ...string) *tui.ProviderBlocks {
	t.Helper()
	var bs []json.RawMessage
	for _, r := range raws {
		bs = append(bs, json.RawMessage(r))
	}
	pb, err := tui.NewProviderBlocks(blocksKey, bs)
	if err != nil {
		t.Fatal(err)
	}
	return pb
}

// callsWithBlocks streams tool calls ({id, name, args}) and ends the reply
// with pb.
func callsWithBlocks(cb llm.StreamEventCallback, pb *tui.ProviderBlocks, calls ...[3]string) {
	for _, c := range calls {
		cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: c[0], ToolName: c[1]})
		cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: c[0], ToolName: c[1], CompleteInput: c[2]})
	}
	cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "tool_calls", ProviderBlocks: pb})
}

func assertBlocks(t *testing.T, msg Message, want *tui.ProviderBlocks, what string) {
	t.Helper()
	got, ok := tui.ReplayBlocks(msg, blocksKey)
	if !ok {
		t.Fatalf("%s: no replayable blocks (have %+v)", what, msg.ProviderBlocks)
	}
	if len(got) != len(want.Blocks) {
		t.Fatalf("%s: %d blocks, want %d", what, len(got), len(want.Blocks))
	}
	for i := range got {
		if !bytes.Equal(got[i], want.Blocks[i]) {
			t.Fatalf("%s: block %d = %s, want %s", what, i, got[i], want.Blocks[i])
		}
	}
}

// W2's shape: a 3-request tool loop whose replies carry thinking blocks with
// signatures. Each assistant message keeps its blocks, and every later
// request sends the earlier ones byte for byte (append-only history).
func TestLoopReplaysProviderBlocksUnchanged(t *testing.T) {
	hermetic(t)
	b := []*tui.ProviderBlocks{
		mustBlocks(t, `{"type":"thinking","thinking":"read <a> first","signature":"S1"}`, `{"type":"tool_use","id":"c1","name":"echo","input":{"k":"a"}}`),
		mustBlocks(t, `{"type":"thinking","thinking":"now b","signature":"S2"}`, `{"type":"tool_use","id":"c2","name":"echo","input":{"k":"b"}}`),
		mustBlocks(t, `{"type":"thinking","thinking":"done","signature":"S3"}`, `{"type":"text","text":"all done"}`),
	}
	s := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		switch n {
		case 0:
			callsWithBlocks(cb, b[0], [3]string{"c1", "echo", `{"k":"a"}`})
		case 1:
			callsWithBlocks(cb, b[1], [3]string{"c2", "echo", `{"k":"b"}`})
		default:
			cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: "all done"})
			cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop", ProviderBlocks: b[2]})
		}
		return nil
	}}
	l := &Loop{Client: s, Tools: newRegistry(&fakeTool{name: "echo", safe: true, readOnly: true}), Limits: DefaultLimits(), SpillDir: t.TempDir()}
	msgs, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopDone {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	// user, assistant(c1), tool, assistant(c2), tool, assistant(text)
	assistants := []int{1, 3, 5}
	if len(msgs) != 6 {
		t.Fatalf("history has %d messages, want 6", len(msgs))
	}
	for i, idx := range assistants {
		assertBlocks(t, msgs[idx], b[i], "recorded reply")
	}
	for n := 1; n <= 2; n++ {
		req := s.request(n)
		for i := 0; i < n; i++ {
			assertBlocks(t, req[assistants[i]], b[i], "earlier reply in a later request")
		}
	}
}

// Blocks hold every call the provider made. When MaxCallsPerTurn drops some,
// replaying them would send tool_use blocks that never got results, so the
// recorded turn keeps no blocks (ruling 6). A reply without blocks records
// none.
func TestLoopDropsBlocksWhenCallsAreCapped(t *testing.T) {
	hermetic(t)
	pb := mustBlocks(t, `{"type":"tool_use","id":"c1","name":"echo","input":{}}`, `{"type":"tool_use","id":"c2","name":"echo","input":{}}`)
	s := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			callsWithBlocks(cb, pb, [3]string{"c1", "echo", `{}`}, [3]string{"c2", "echo", `{"k":"x"}`})
			return nil
		}
		sayText(cb, "ok", nil)
		return nil
	}}
	lim := DefaultLimits()
	lim.MaxCallsPerTurn = 1
	l := &Loop{Client: s, Tools: newRegistry(&fakeTool{name: "echo", safe: true, readOnly: true}), Limits: lim, SpillDir: t.TempDir()}
	msgs, _, err := l.Run(context.Background(), userMsg("go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("recorded %d calls, want 1 (capped)", len(msgs[1].ToolCalls))
	}
	if msgs[1].ProviderBlocks != nil {
		t.Fatal("a turn whose calls were capped kept blocks that name the dropped call")
	}
	if last := msgs[len(msgs)-1]; last.Content != "ok" || last.ProviderBlocks != nil {
		t.Fatalf("a reply without blocks = %+v", last)
	}
}

// Calls parsed from the reply text (Limits.TextToolCalls) have no native
// tool_use blocks behind them; replaying the blocks would drop the calls the
// next tool results answer, so the turn keeps no blocks.
func TestLoopKeepsNoBlocksForTextToolCalls(t *testing.T) {
	hermetic(t)
	pb := mustBlocks(t, `{"type":"text","text":"calling"}`)
	s := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: `<tool_call>{"name":"echo","arguments":{"k":"a"}}</tool_call>`})
			cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop", ProviderBlocks: pb})
			return nil
		}
		sayText(cb, "ok", nil)
		return nil
	}}
	lim := DefaultLimits()
	lim.TextToolCalls = true
	l := &Loop{Client: s, Tools: newRegistry(&fakeTool{name: "echo", safe: true, readOnly: true}), Limits: lim, SpillDir: t.TempDir()}
	msgs, _, err := l.Run(context.Background(), userMsg("go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 3 || !strings.HasPrefix(msgs[2].Content, "[Tool Result: echo]") {
		t.Fatalf("the text tool call did not run: %+v", msgs)
	}
	if msgs[1].ProviderBlocks != nil {
		t.Fatal("a turn with text-format tool calls kept provider blocks")
	}
}

// A backend that had to drop replayed blocks (the provider rejected them,
// W2's strip-and-retry) says so with BlocksRejected; the loop strips every
// message's blocks so they are not sent again, and keeps the new reply's.
func TestLoopStripsBlocksWhenTheProviderRejectsThem(t *testing.T) {
	hermetic(t)
	first := mustBlocks(t, `{"type":"thinking","thinking":"a","signature":"S1"}`, `{"type":"tool_use","id":"c1","name":"echo","input":{}}`)
	fresh := mustBlocks(t, `{"type":"text","text":"ok"}`)
	s := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			callsWithBlocks(cb, first, [3]string{"c1", "echo", `{}`})
			return nil
		}
		cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: "ok"})
		cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop", ProviderBlocks: fresh, BlocksRejected: true})
		return nil
	}}
	l := &Loop{Client: s, Tools: newRegistry(&fakeTool{name: "echo", safe: true, readOnly: true}), Limits: DefaultLimits(), SpillDir: t.TempDir()}
	done := collect(l)
	msgs, _, err := l.Run(context.Background(), userMsg("go"))
	if err != nil {
		t.Fatal(err)
	}
	evs := done()
	if _, ok := tui.ReplayBlocks(s.request(1)[1], blocksKey); !ok {
		t.Fatal("the second request should still have replayed the first reply's blocks")
	}
	if msgs[1].ProviderBlocks != nil {
		t.Fatal("rejected blocks stayed in the history")
	}
	assertBlocks(t, msgs[len(msgs)-1], fresh, "the reply that reported the rejection")
	var last []Message
	for _, ev := range evs {
		if ev.Kind == EventTurnEnd {
			last = ev.History
		}
	}
	if len(last) != len(msgs) || last[1].ProviderBlocks != nil {
		t.Fatal("the turn-end snapshot (what the chat syncs) still carries the rejected blocks")
	}
}
