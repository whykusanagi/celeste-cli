package loop

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

func userMsg(s string) []Message { return []Message{{Role: "user", Content: s}} }

func fakeLoop(t *testing.T, turns ...fakeprovider.Turn) (*Loop, *fakeprovider.Server) {
	t.Helper()
	hermetic(t)
	srv := fakeprovider.NewOpenAI(t, turns...)
	reg := newRegistry(&fakeTool{name: "echo", safe: true, readOnly: true, run: func(_ context.Context, in map[string]any) (tools.ToolResult, error) {
		k, _ := in["k"].(string)
		return tools.ToolResult{Content: "echo:" + k}, nil
	}})
	return &Loop{Client: newClient(srv, reg), Tools: reg, Limits: DefaultLimits(), SpillDir: t.TempDir()}, srv
}

func TestLoopTextOnlyTurnIsDone(t *testing.T) {
	l, _ := fakeLoop(t, fakeprovider.Turn{Text: "hello"})
	msgs, res, err := l.Run(context.Background(), userMsg("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if res.StopReason != StopDone || res.FinalText != "hello" || res.Turns != 1 || res.NoToolTurns != 1 {
		t.Fatalf("res = %+v", res)
	}
	if len(msgs) != 2 || msgs[1].Role != "assistant" || msgs[1].Content != "hello" {
		t.Fatalf("msgs = %+v", msgs)
	}
}

func TestLoopToolRoundTrip(t *testing.T) {
	l, srv := fakeLoop(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c1", Name: "echo", Args: `{"k":"a"}`}}},
		fakeprovider.Turn{Text: "done"},
	)
	history := userMsg("go")
	msgs, res, err := l.Run(context.Background(), history)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, ",") != "user,assistant,tool,assistant" {
		t.Fatalf("roles = %v", roles)
	}
	if msgs[1].ToolCalls[0].ID != "c1" || msgs[2].ToolCallID != "c1" || msgs[2].Content != "echo:a" {
		t.Fatalf("msgs = %+v", msgs)
	}
	if res.StopReason != StopDone || res.ToolCalls != 1 || res.Turns != 2 || res.FinalText != "done" {
		t.Fatalf("res = %+v", res)
	}
	if len(history) != 1 {
		t.Fatal("Run must not modify the caller's history")
	}
	body := srv.Requests()[1].Body
	if !strings.Contains(toJSON(body["messages"]), "echo:a") {
		t.Fatal("the second request must carry the tool result")
	}
}

func TestLoopEventsInOrder(t *testing.T) {
	l, _ := fakeLoop(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c1", Name: "echo", Args: `{}`}}},
		fakeprovider.Turn{Text: "done"},
	)
	wait := collect(l)
	if _, _, err := l.Run(context.Background(), userMsg("go")); err != nil {
		t.Fatal(err)
	}
	evs := wait()
	want := []EventKind{EventTurnStart, EventAssistant, EventCallsRecorded, EventToolStart, EventToolResult, EventTurnEnd,
		EventTurnStart, EventAssistant, EventTurnEnd, EventDone}
	if got := kinds(evs); !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v\nwant %v", got, want)
	}
	var sawDelta bool
	for _, e := range evs {
		if e.Kind == EventTextDelta && e.Text != "" {
			sawDelta = true
		}
	}
	if !sawDelta {
		t.Fatal("no EventTextDelta for the streamed reply")
	}
	if last := evs[len(evs)-1]; last.Result.StopReason != StopDone {
		t.Fatalf("EventDone result = %+v", last.Result)
	}
}

func TestLoopTurnCap(t *testing.T) {
	l, srv := fakeLoop(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c1", Name: "echo", Args: `{"k":"1"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c2", Name: "echo", Args: `{"k":"2"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "c3", Name: "echo", Args: `{"k":"3"}`}}},
	)
	l.Limits.MaxTurns = 2
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopCap || res.Turns != 2 || len(srv.Requests()) != 2 {
		t.Fatalf("res=%+v err=%v requests=%d", res, err, len(srv.Requests()))
	}
}

// The cap is applied before the assistant turn is recorded, so declared
// tool_calls always equal returned results (the Sakana Fugu 400).
func TestLoopCapsCallsPerTurn(t *testing.T) {
	l, _ := fakeLoop(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{
			{ID: "a", Name: "echo", Args: `{"k":"1"}`},
			{ID: "b", Name: "echo", Args: `{"k":"2"}`},
			{ID: "c", Name: "echo", Args: `{"k":"3"}`},
		}},
		fakeprovider.Turn{Text: "done"},
	)
	l.Limits.MaxCallsPerTurn = 2
	msgs, _, err := l.Run(context.Background(), userMsg("go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs[1].ToolCalls) != 2 || msgs[2].ToolCallID != "a" || msgs[3].ToolCallID != "b" || msgs[4].Role != "assistant" {
		t.Fatalf("msgs = %+v", msgs)
	}
}

func TestLoopInterruptKeepsHistoryPaired(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	block := &fakeTool{name: "block", safe: true, readOnly: true, run: func(c context.Context, _ map[string]any) (tools.ToolResult, error) {
		cancel() // Esc while the batch runs
		<-c.Done()
		return tools.ToolResult{}, c.Err()
	}}
	stub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: "x", ToolName: "block"})
		cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: "x", ToolName: "block", CompleteInput: `{}`})
		cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: "y", ToolName: "block"})
		cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: "y", ToolName: "block", CompleteInput: `{}`})
		cb(llm.StreamEvent{Type: llm.EventMessageDone})
		return nil
	}}
	l := &Loop{Client: stub, Tools: newRegistry(block), Limits: DefaultLimits(), SpillDir: t.TempDir()}
	msgs, res, err := l.Run(ctx, userMsg("go"))
	if !errors.Is(err, context.Canceled) || res.StopReason != StopInterrupted {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if n := len(msgs); n != 4 || msgs[1].Role != "assistant" || msgs[2].ToolCallID != "x" || msgs[3].ToolCallID != "y" {
		t.Fatalf("history must pair both calls with results, got %+v", msgs)
	}
	if stub.calls != 1 {
		t.Fatalf("requests = %d, want no request after the interrupt", stub.calls)
	}
}

func TestLoopRequestTimeout(t *testing.T) {
	stub := &stubLLM{reply: func(_ int, ctx context.Context, _ llm.StreamEventCallback) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	l := &Loop{Client: stub, Tools: newRegistry(), Limits: DefaultLimits()}
	l.Limits.RequestTimeout = 50 * time.Millisecond
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if !errors.Is(err, ErrTurnTimeout) || res.StopReason != StopError {
		t.Fatalf("res=%+v err=%v, want a per-turn timeout", res, err)
	}
	var tte *TurnTimeoutError
	if !errors.As(err, &tte) || tte.Timeout != 50*time.Millisecond {
		t.Fatalf("err = %#v", err)
	}
}

func TestLoopProviderErrorStops(t *testing.T) {
	l, _ := fakeLoop(t, fakeprovider.Turn{Status: 401, Body: `{"error":{"message":"bad key"}}`})
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if err == nil || res.StopReason != StopError {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
