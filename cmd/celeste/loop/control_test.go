package loop

import (
	"context"
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
