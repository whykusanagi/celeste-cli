package loop

import (
	"context"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func textStub() *stubLLM {
	return &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		sayText(cb, "ok", nil)
		return nil
	}}
}

func withContext(ctx string) PromptCheck {
	return func(_ context.Context, m Message) (Message, PromptVerdict, error) {
		m.Metadata = map[string]any{tui.MetaHookContext: ctx}
		return m, PromptVerdict{}, nil
	}
}

func blockIf(content, reason string) PromptCheck {
	return func(_ context.Context, m Message) (Message, PromptVerdict, error) {
		if m.Content == content {
			return m, PromptVerdict{Blocked: true, Reason: reason}, nil
		}
		return m, PromptVerdict{}, nil
	}
}

func isDone(m Message) bool {
	d, _ := m.Metadata[tui.MetaPromptHookDone].(bool)
	return d
}

// The hook context reaches the provider; the history keeps the prompt as
// typed, marked checked, with the context in metadata.
func TestLoopCheckPromptMarksAndSendsContext(t *testing.T) {
	stub := textStub()
	l := &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits(), CheckPrompt: withContext("CTX")}
	msgs, _, err := l.Run(context.Background(), userMsg("go"))
	if err != nil {
		t.Fatal(err)
	}
	if got := lastUser(stub.request(0)); got != "go\n\n<hook-context>\nCTX\n</hook-context>" {
		t.Fatalf("sent %q", got)
	}
	if msgs[0].Content != "go" || !isDone(msgs[0]) || msgs[0].Metadata[tui.MetaHookContext] != "CTX" {
		t.Fatalf("history user message = %+v", msgs[0])
	}
}

// A blocked prompt with nothing allowed: no request, StopBlocked, and an
// event naming the message and the reason.
func TestLoopBlockedPromptStopsBeforeAnyRequest(t *testing.T) {
	stub := textStub()
	l := &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits(), CheckPrompt: blockIf("go", "not today")}
	wait := collect(l)
	msgs, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopBlocked || stub.calls != 0 || len(msgs) != 0 {
		t.Fatalf("res=%+v err=%v calls=%d msgs=%v", res, err, stub.calls, msgs)
	}
	var blocked bool
	for _, e := range wait() {
		blocked = blocked || (e.Kind == EventPromptBlocked && e.Msg.Content == "go" && e.Text == "not today")
	}
	if !blocked {
		t.Fatal("no EventPromptBlocked")
	}
}

// Only unchecked, visible user messages are checked; one allowed prompt
// keeps the run going when another is blocked.
func TestLoopCheckPromptSkipsCheckedAndHidden(t *testing.T) {
	var checked []string
	stub := textStub()
	l := &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits(),
		CheckPrompt: func(_ context.Context, m Message) (Message, PromptVerdict, error) {
			checked = append(checked, m.Content)
			return m, PromptVerdict{Blocked: m.Content == "bad"}, nil
		}}
	history := []Message{
		{Role: "user", Content: "old", Metadata: map[string]any{tui.MetaPromptHookDone: true}},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "directive", Metadata: map[string]any{"hidden": true}},
		{Role: "user", Content: "bad"},
		{Role: "user", Content: "new"},
	}
	_, res, err := l.Run(context.Background(), history)
	if err != nil || res.StopReason != StopDone {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(checked) != 2 || checked[0] != "bad" || checked[1] != "new" {
		t.Fatalf("checked %v, want [bad new]", checked)
	}
	for _, m := range stub.request(0) {
		if m.Content == "bad" {
			t.Fatal("the blocked prompt was sent")
		}
	}
}

// The checked prompts reach the consumer before the first request, so an
// interrupt during that request does not leave the chat's copy unchecked
// (the hook would run again on the next send).
func TestLoopReportsCheckedPromptsBeforeTheFirstRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stub := &stubLLM{reply: func(int, context.Context, llm.StreamEventCallback) error {
		cancel() // Esc during the first request
		return context.Canceled
	}}
	l := &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits(), CheckPrompt: withContext("CTX")}
	wait := collect(l)
	_, res, _ := l.Run(ctx, userMsg("go"))
	if res.StopReason != StopInterrupted {
		t.Fatalf("res = %+v", res)
	}
	var kinds []EventKind
	var checked []Message
	for _, e := range wait() {
		kinds = append(kinds, e.Kind)
		if e.Kind == EventPromptsChecked {
			checked = e.History
		}
	}
	if len(kinds) < 2 || kinds[0] != EventPromptsChecked || kinds[1] != EventTurnStart {
		t.Fatalf("events = %v, want EventPromptsChecked before the first EventTurnStart", kinds)
	}
	if len(checked) != 1 || !isDone(checked[0]) || checked[0].Metadata[tui.MetaHookContext] != "CTX" {
		t.Fatalf("checked history = %+v", checked)
	}
}

// An interrupt during the check is not a verdict: no request, no block
// event, and the prompt stays unchecked for the next send.
func TestLoopInterruptedPromptCheckKeepsThePrompt(t *testing.T) {
	stub := textStub()
	ctx, cancel := context.WithCancel(context.Background())
	l := &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits(),
		CheckPrompt: func(ctx context.Context, m Message) (Message, PromptVerdict, error) {
			cancel()
			return m, PromptVerdict{}, ctx.Err()
		}}
	wait := collect(l)
	msgs, res, err := l.Run(ctx, userMsg("keep me"))
	if err == nil || res.StopReason != StopInterrupted || stub.calls != 0 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, stub.calls)
	}
	if len(msgs) != 1 || isDone(msgs[0]) {
		t.Fatalf("msgs = %+v, want the prompt kept unchecked", msgs)
	}
	for _, e := range wait() {
		if e.Kind == EventPromptBlocked {
			t.Fatal("an interrupted check was reported as a block")
		}
	}
}

// Steers are checked when they join: the blocked one never reaches the
// provider, the allowed one joins marked checked, and the run goes on.
func TestLoopSteersAreChecked(t *testing.T) {
	var l *Loop
	steerer := &fakeTool{name: "s", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		l.Steer("STEER-A")
		l.Steer("steer B")
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
	l = &Loop{Client: stub, Tools: newRegistry(steerer), Limits: DefaultLimits(), CheckPrompt: blockIf("STEER-A", "no A")}
	wait := collect(l)
	msgs, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopDone {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	for _, m := range stub.request(1) {
		if m.Content == "STEER-A" {
			t.Fatal("the blocked steer was sent")
		}
	}
	if lastUser(stub.request(1)) != "steer B" {
		t.Fatalf("second request = %+v", stub.request(1))
	}
	var steered, blocked bool
	for _, e := range wait() {
		steered = steered || (e.Kind == EventSteered && e.Msg.Content == "steer B" && isDone(e.Msg))
		blocked = blocked || (e.Kind == EventSteerBlocked && e.Msg.Content == "STEER-A" && e.Text == "no A")
	}
	if !steered || !blocked {
		t.Fatalf("steered=%v blocked=%v", steered, blocked)
	}
	for _, m := range msgs {
		if m.Content == "STEER-A" {
			t.Fatal("the blocked steer is in the history")
		}
	}
}

// Steers typed after the last tool boundary of an interrupted run are handed
// back, once.
func TestLoopTakeSteersAfterInterrupt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var l *Loop
	steerer := &fakeTool{name: "s", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		l.Steer("late")
		cancel()
		return tools.ToolResult{Content: "done"}, nil
	}}
	stub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		callTool(cb, "c1", "s", `{}`)
		return nil
	}}
	l = &Loop{Client: stub, Tools: newRegistry(steerer), Limits: DefaultLimits()}
	_, res, _ := l.Run(ctx, userMsg("go"))
	if res.StopReason != StopInterrupted {
		t.Fatalf("res = %+v", res)
	}
	if got := l.TakeSteers(); len(got) != 1 || got[0] != "late" {
		t.Fatalf("TakeSteers = %v, want [late]", got)
	}
	if got := l.TakeSteers(); len(got) != 0 {
		t.Fatalf("second TakeSteers = %v, want none", got)
	}
}

// A steer blocked during the final reply ends the run instead of asking the
// model to answer the same thing again.
func TestLoopBlockedSteerDuringFinalReplyEndsTheRun(t *testing.T) {
	var l *Loop
	stub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			l.Steer("BLOCKED")
		}
		sayText(cb, "reply", nil)
		return nil
	}}
	l = &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits(), CheckPrompt: blockIf("BLOCKED", "no")}
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopDone || stub.calls != 1 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, stub.calls)
	}
}

// Without CheckPrompt (agent, MCP) messages go out untouched.
func TestLoopWithoutCheckPromptLeavesMessagesAlone(t *testing.T) {
	stub := textStub()
	l := &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits()}
	msgs, _, err := l.Run(context.Background(), []Message{{Role: "user", Content: "go", Timestamp: time.Now()}})
	if err != nil || msgs[0].Metadata != nil {
		t.Fatalf("msgs[0] = %+v err=%v", msgs[0], err)
	}
}

// A steer check cut short by an interrupt is not a verdict: the steer and
// the ones after it go back in the queue for TakeSteers, and no request is
// sent with them missing. (Added in implementation: the plan's tests did not
// pin requeue.)
func TestLoopInterruptedSteerCheckRequeues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var l *Loop
	steerer := &fakeTool{name: "s", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		l.Steer("a")
		l.Steer("b")
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
	l = &Loop{Client: stub, Tools: newRegistry(steerer), Limits: DefaultLimits(),
		CheckPrompt: func(ctx context.Context, m Message) (Message, PromptVerdict, error) {
			if m.Content == "a" {
				cancel()
				return m, PromptVerdict{}, ctx.Err()
			}
			return m, PromptVerdict{}, nil
		}}
	msgs, res, err := l.Run(ctx, userMsg("go"))
	if err == nil || res.StopReason != StopInterrupted || stub.calls != 1 || res.Turns != 1 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, stub.calls)
	}
	for _, m := range msgs {
		if m.Content == "a" || m.Content == "b" {
			t.Fatalf("a requeued steer is in the history: %+v", msgs)
		}
	}
	if got := l.TakeSteers(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("TakeSteers = %v, want [a b]", got)
	}
}

// A steer that arrives during the final reply of the last allowed turn:
// without CheckPrompt the loop goes round to answer it and hits the cap,
// the steer still queued; with CheckPrompt the run ends StopDone and
// TakeSteers hands it back.
func TestLoopLateSteerAtMaxTurns(t *testing.T) {
	for _, withCheck := range []bool{false, true} {
		var l *Loop
		stub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
			l.Steer("late")
			sayText(cb, "reply", nil)
			return nil
		}}
		lim := DefaultLimits()
		lim.MaxTurns = 1
		l = &Loop{Client: stub, Tools: newRegistry(), Limits: lim}
		want := StopCap
		if withCheck {
			l.CheckPrompt = withContext("CTX")
			want = StopDone
		}
		_, res, err := l.Run(context.Background(), userMsg("go"))
		if err != nil || res.StopReason != want || stub.calls != 1 {
			t.Fatalf("check=%v: res=%+v err=%v calls=%d, want %v", withCheck, res, err, stub.calls, want)
		}
		if got := l.TakeSteers(); len(got) != 1 || got[0] != "late" {
			t.Fatalf("check=%v: TakeSteers = %v, want [late]", withCheck, got)
		}
	}
}

// EventPromptsChecked means a prompt was checked: a history whose prompts
// are all already checked emits none.
func TestLoopNoPromptsCheckedEventWhenNothingToCheck(t *testing.T) {
	l := &Loop{Client: textStub(), Tools: newRegistry(), Limits: DefaultLimits(), CheckPrompt: withContext("CTX")}
	wait := collect(l)
	history := []Message{markChecked(Message{Role: "user", Content: "go"})}
	if _, _, err := l.Run(context.Background(), history); err != nil {
		t.Fatal(err)
	}
	for _, e := range wait() {
		if e.Kind == EventPromptsChecked {
			t.Fatal("EventPromptsChecked emitted with nothing to check")
		}
	}
}

// PromptGate hands the prompt the asking call's context, so an interactive
// prompt knows which run asked and whether it has ended (2.0 F2e).
func TestPromptGatePassesTheCallsContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "run-9")
	var got context.Context
	g := PromptGate(func(req tools.PermissionRequest) tools.PermissionResponse {
		got = req.Context
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	if r := g.Ask(ctx, tools.PermissionRequest{ToolName: "write_file"}); r.Decision != "allow_once" {
		t.Fatalf("decision = %q", r.Decision)
	}
	if got == nil || got.Value(key{}) != "run-9" {
		t.Fatalf("prompt got context %v, want the call's", got)
	}
}
