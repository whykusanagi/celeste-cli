package loop

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func lastUser(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

func TestLoopSteerJoinsAtToolBoundary(t *testing.T) {
	var l *Loop
	steerer := &fakeTool{name: "s", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		l.Steer("also check the tests")
		return tools.ToolResult{Content: "done"}, nil
	}}
	stub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			callTool(cb, "c1", "s", `{}`)
			return nil
		}
		sayText(cb, "ok", nil)
		return nil
	}}
	l = &Loop{Client: stub, Tools: newRegistry(steerer), Limits: DefaultLimits()}
	wait := collect(l)
	if _, _, err := l.Run(context.Background(), userMsg("go")); err != nil {
		t.Fatal(err)
	}
	second := stub.request(1)
	if second[len(second)-2].Role != "tool" || lastUser(second) != "also check the tests" || second[len(second)-1].Role != "user" {
		t.Fatalf("second request = %+v, want the steer after the tool result", second)
	}
	var steered bool
	for _, e := range wait() {
		steered = steered || (e.Kind == EventSteered && e.Text == "also check the tests")
	}
	if !steered {
		t.Fatal("no EventSteered")
	}
}

func TestLoopSteerDuringFinalReplyRunsAnotherTurn(t *testing.T) {
	var l *Loop
	stub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			l.Steer("wait, one more thing") // typed while the reply streams
		}
		sayText(cb, fmt.Sprintf("reply %d", n), nil)
		return nil
	}}
	l = &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits()}
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.Turns != 2 || res.FinalText != "reply 1" || res.NoToolTurns != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if lastUser(stub.request(1)) != "wait, one more thing" {
		t.Fatalf("second request = %+v", stub.request(1))
	}
}

// countingCompactor records its calls and changes the history only when
// forced and change is set.
type countingCompactor struct {
	calls  int
	usages []*llm.TokenUsage
	forced int
	change bool
}

func (c *countingCompactor) Compact(_ context.Context, h []Message, u *llm.TokenUsage, force bool) ([]Message, []string, bool) {
	c.calls++
	c.usages = append(c.usages, u)
	if !force {
		return h, nil, false
	}
	c.forced++
	if !c.change {
		return h, nil, false
	}
	return h[len(h)-1:], []string{"dropped old turns"}, true
}

func TestLoopCompactorRunsBeforeEachRequest(t *testing.T) {
	usage := &llm.TokenUsage{PromptTokens: 100, CompletionTokens: 5}
	stub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: "c", ToolName: "x"})
			cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: "c", ToolName: "x", CompleteInput: `{}`})
			cb(llm.StreamEvent{Type: llm.EventMessageDone, Usage: usage})
			return nil
		}
		sayText(cb, "done", nil)
		return nil
	}}
	c := &countingCompactor{}
	l := &Loop{Client: stub, Tools: newRegistry(&fakeTool{name: "x"}), Limits: DefaultLimits(), Compact: c}
	if _, _, err := l.Run(context.Background(), userMsg("go")); err != nil {
		t.Fatal(err)
	}
	if c.calls != 2 || c.usages[0] != nil || c.usages[1] != usage {
		t.Fatalf("calls=%d usages=%v, want nil then the first request's usage", c.calls, c.usages)
	}
}

func TestLoopOverflowCompactsAndRetriesOnce(t *testing.T) {
	stub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			return fmt.Errorf("prompt is too long: %w", llm.ErrContextOverflow)
		}
		sayText(cb, "fits now", nil)
		return nil
	}}
	c := &countingCompactor{change: true}
	l := &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits(), Compact: c}
	wait := collect(l)
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopDone || res.Turns != 1 || c.forced != 1 {
		t.Fatalf("res=%+v err=%v forced=%d", res, err, c.forced)
	}
	var note bool
	for _, e := range wait() {
		note = note || (e.Kind == EventCompacted && e.Text == "dropped old turns")
	}
	if !note {
		t.Fatal("no EventCompacted for the forced compaction")
	}
}

func TestLoopOverflowWithNothingToCompactFails(t *testing.T) {
	stub := &stubLLM{reply: func(int, context.Context, llm.StreamEventCallback) error {
		return fmt.Errorf("prompt is too long: %w", llm.ErrContextOverflow)
	}}
	l := &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits(), Compact: &countingCompactor{}}
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if err == nil || res.StopReason != StopError || stub.calls != 1 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, stub.calls)
	}
}

// The retry is one-shot: even when the compactor keeps reporting a change,
// a second overflow on the same turn must not be retried again (#174). A
// no-op in place of the overflowRetried=true assignment would retry forever
// here (turn-- keeps the turn counter from ever reaching MaxTurns).
func TestLoopOverflowRetriesOnlyOnce(t *testing.T) {
	stub := &stubLLM{reply: func(int, context.Context, llm.StreamEventCallback) error {
		return fmt.Errorf("prompt is too long: %w", llm.ErrContextOverflow)
	}}
	c := &countingCompactor{change: true}
	l := &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits(), Compact: c}
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if stub.calls != 2 {
		t.Fatalf("requests = %d, want exactly 2 (the original attempt plus one retry)", stub.calls)
	}
	if res.StopReason != StopError {
		t.Fatalf("StopReason = %v, want StopError", res.StopReason)
	}
	if !errors.Is(err, llm.ErrContextOverflow) {
		t.Fatalf("err = %v, want it to wrap the real llm.ErrContextOverflow sentinel", err)
	}
}

// overflowRetried must reset once a turn succeeds, so a later turn's own
// overflow gets its own one-shot retry rather than being refused outright.
func TestLoopOverflowRetryResetsAfterSuccessfulTurn(t *testing.T) {
	stub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		switch n {
		case 0:
			return fmt.Errorf("prompt is too long: %w", llm.ErrContextOverflow) // turn 1, first attempt
		case 1:
			callTool(cb, "c1", "x", `{}`) // turn 1's retry succeeds, forcing a turn 2
			return nil
		case 2:
			return fmt.Errorf("prompt is too long: %w", llm.ErrContextOverflow) // turn 2, first attempt: its own retry
		default:
			sayText(cb, "done", nil) // turn 2's retry succeeds
			return nil
		}
	}}
	c := &countingCompactor{change: true}
	l := &Loop{Client: stub, Tools: newRegistry(&fakeTool{name: "x"}), Limits: DefaultLimits(), Compact: c}
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil {
		t.Fatal(err)
	}
	if stub.calls != 4 {
		t.Fatalf("requests = %d, want 4 (turn 1 overflow+retry, turn 2 overflow+retry)", stub.calls)
	}
	if res.StopReason != StopDone || res.Turns != 2 || res.FinalText != "done" {
		t.Fatalf("res = %+v", res)
	}
	if c.forced != 2 {
		t.Fatalf("compactor forced calls = %d, want 2: each turn's overflow must get its own retry", c.forced)
	}
}
