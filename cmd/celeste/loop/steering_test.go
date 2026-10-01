package loop

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// fakeSteering interrupts on text containing onText and on calls to
// onCall, and hands out the reminders queued in due.
type fakeSteering struct {
	mu        sync.Mutex
	onText    string
	onCall    string
	due       map[Boundary][]Reminder
	requests  []bool // per request: was an interrupt function given
	observed  []EventKind
	interrupt func()
	callChecks int
}

func (f *fakeSteering) Request(_ int, interrupt func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, interrupt != nil)
	f.interrupt = interrupt
}

func (f *fakeSteering) Observe(ev Event) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observed = append(f.observed, ev.Kind)
	if ev.Kind == EventTextDelta && f.onText != "" && strings.Contains(ev.Text, f.onText) {
		f.queue(BoundaryRetry, Reminder{Source: "rule:text", Text: "no " + f.onText})
		return true
	}
	return false
}

func (f *fakeSteering) Calls(_ int, calls []ToolCall) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callChecks++
	for _, c := range calls {
		if c.Name == f.onCall {
			f.queue(BoundaryRetry, Reminder{Source: "rule:call", Text: "not " + c.Name + " " + toJSON(c.Input)})
			return true
		}
	}
	return false
}

func (f *fakeSteering) queue(b Boundary, r Reminder) {
	if f.due == nil {
		f.due = map[Boundary][]Reminder{}
	}
	f.due[b] = append(f.due[b], r)
}

func (f *fakeSteering) Reminders(b Boundary) []Reminder {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.due[b]
	delete(f.due, b)
	return r
}

// A text rule interrupts the reply: the turn re-runs with the reminder as
// a hidden <system-reminder> message, and the dropped reply never reaches
// the history.
func TestSteeringInterruptRerunsTheTurnWithTheReminder(t *testing.T) {
	llmStub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			sayText(cb, "Audio saved: /tmp/x.mp3", nil)
			return nil
		}
		sayText(cb, "No audio was made.", nil)
		return nil
	}}
	st := &fakeSteering{onText: "Audio saved:"}
	l := &Loop{Client: llmStub, Tools: newRegistry(), Limits: DefaultLimits(), Steering: st}
	wait := collect(l)
	msgs, res, err := l.Run(context.Background(), []Message{{Role: "user", Content: "say it"}})
	evs := wait()
	if err != nil || res.StopReason != StopDone || res.FinalText != "No audio was made." || res.Turns != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(msgs) != 3 || msgs[1].Metadata[MetaReminder] != "rule:text" || !strings.Contains(msgs[1].Content, "<system-reminder>\nno Audio saved:\n</system-reminder>") {
		t.Fatalf("history = %+v", msgs)
	}
	if hidden, _ := msgs[1].Metadata["hidden"].(bool); !hidden || needsCheck(msgs[1]) {
		t.Error("a reminder must be hidden and never go through UserPromptSubmit")
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "Audio saved:") && m.Role == "assistant" {
			t.Error("the interrupted reply reached the history")
		}
	}
	if second := llmStub.request(1); second[len(second)-1].Metadata[MetaReminder] != "rule:text" {
		t.Errorf("the re-run request did not end with the reminder: %+v", second)
	}
	got := strings.Join(kindNames(kinds(evs)), ",")
	if !strings.Contains(got, "TurnStart,RuleInterrupt,Rule,TurnStart,Assistant") {
		t.Errorf("events = %s", got)
	}
}

// A rule on tool arguments drops the turn before any call runs.
func TestSteeringCallsRuleDropsTheTurnBeforeItRuns(t *testing.T) {
	ran := 0
	bash := &fakeTool{name: "bash", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		ran++
		return tools.ToolResult{Content: "ok"}, nil
	}}
	llmStub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			callTool(cb, "c1", "bash", `{"command":"rm -rf build"}`)
			return nil
		}
		sayText(cb, "I will ask first.", nil)
		return nil
	}}
	st := &fakeSteering{onCall: "bash"}
	l := &Loop{Client: llmStub, Tools: newRegistry(bash), Limits: DefaultLimits(), Steering: st}
	msgs, res, err := l.Run(context.Background(), []Message{{Role: "user", Content: "clean"}})
	if err != nil || res.FinalText != "I will ask first." || ran != 0 {
		t.Fatalf("res=%+v err=%v ran=%d", res, err, ran)
	}
	for _, m := range msgs {
		if len(m.ToolCalls) > 0 || m.Role == "tool" {
			t.Fatalf("the dropped turn's calls were recorded: %+v", msgs)
		}
	}
	if !strings.Contains(msgs[1].Content, `not bash {"command":"rm -rf build"}`) {
		t.Errorf("reminder = %q", msgs[1].Content)
	}
}

// Past MaxRuleInterrupts re-runs of one turn, steering fails open: the
// next request gets no interrupt function and its reply goes through.
func TestSteeringInterruptsAreCappedPerTurn(t *testing.T) {
	llmStub := &stubLLM{reply: func(_ int, _ context.Context, cb llm.StreamEventCallback) error {
		sayText(cb, "Audio saved: again", nil)
		return nil
	}}
	st := &fakeSteering{onText: "Audio saved:"}
	l := &Loop{Client: llmStub, Tools: newRegistry(), Limits: DefaultLimits(), Steering: st}
	_, res, err := l.Run(context.Background(), []Message{{Role: "user", Content: "x"}})
	if err != nil || res.FinalText != "Audio saved: again" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if llmStub.calls != DefaultMaxRuleInterrupts+1 {
		t.Errorf("requests = %d, want %d", llmStub.calls, DefaultMaxRuleInterrupts+1)
	}
	if want := []bool{true, true, false}; toJSON(st.requests) != toJSON(want) {
		t.Errorf("interrupt functions given = %v, want %v", st.requests, want)
	}
}

// Queued reminders join before the Run's first request; appended ones
// before the request after a tool turn.
func TestSteeringRemindersJoinAtTheirBoundary(t *testing.T) {
	llmStub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			callTool(cb, "c1", "read", `{}`)
			return nil
		}
		sayText(cb, "done", nil)
		return nil
	}}
	st := &fakeSteering{due: map[Boundary][]Reminder{
		BoundaryRun:   {{Source: "rule:queued", Text: "q"}},
		BoundaryTools: {{Source: "watchdog", Text: "w"}},
	}}
	l := &Loop{Client: llmStub, Tools: newRegistry(&fakeTool{name: "read", readOnly: true}), Limits: DefaultLimits(), Steering: st}
	if _, _, err := l.Run(context.Background(), []Message{{Role: "user", Content: "go"}}); err != nil {
		t.Fatal(err)
	}
	first, second := llmStub.request(0), llmStub.request(1)
	if first[len(first)-1].Metadata[MetaReminder] != "rule:queued" {
		t.Errorf("first request = %+v", first)
	}
	if last := second[len(second)-1]; last.Metadata[MetaReminder] != "watchdog" || second[len(second)-2].Role != "tool" {
		t.Errorf("second request = %+v", second)
	}
}

// An interrupt that arrives after the request returned does nothing.
func TestSteeringLateInterruptIsANoOp(t *testing.T) {
	llmStub := &stubLLM{reply: func(_ int, _ context.Context, cb llm.StreamEventCallback) error {
		sayText(cb, "fine", nil)
		return nil
	}}
	st := &fakeSteering{}
	l := &Loop{Client: llmStub, Tools: newRegistry(), Limits: DefaultLimits(), Steering: st}
	if _, res, err := l.Run(context.Background(), []Message{{Role: "user", Content: "x"}}); err != nil || res.FinalText != "fine" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	st.interrupt() // the watchdog's verdict, too late
	if llmStub.calls != 1 {
		t.Errorf("requests = %d", llmStub.calls)
	}
}

func kindNames(ks []EventKind) []string {
	names := map[EventKind]string{EventTurnStart: "TurnStart", EventAssistant: "Assistant", EventTurnEnd: "TurnEnd",
		EventDone: "Done", EventRule: "Rule", EventRuleInterrupt: "RuleInterrupt", EventToolStart: "ToolStart", EventToolResult: "ToolResult"}
	out := make([]string, len(ks))
	for i, k := range ks {
		if n, ok := names[k]; ok {
			out[i] = n
		} else {
			out[i] = "?"
		}
	}
	return out
}

// endSteering also implements StreamEnder: its verdict comes only when the
// stream ends (a batched matcher's last scan, W3-1 review I1).
type endSteering struct {
	fakeSteering
	ends int
}

func (e *endSteering) EndStream() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ends++
	if e.ends == 1 {
		e.queue(BoundaryRetry, Reminder{Source: "rule:end", Text: "late"})
		return true
	}
	return false
}

// A verdict at the end of the stream still drops the reply and re-runs the
// turn, even though the provider already sent the whole reply.
func TestSteeringEndOfStreamVerdictInterrupts(t *testing.T) {
	llmStub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			sayText(cb, "Audio saved: x", nil)
			return nil
		}
		sayText(cb, "No audio.", nil)
		return nil
	}}
	st := &endSteering{}
	l := &Loop{Client: llmStub, Tools: newRegistry(), Limits: DefaultLimits(), Steering: st}
	msgs, res, err := l.Run(context.Background(), []Message{{Role: "user", Content: "x"}})
	if err != nil || res.FinalText != "No audio." || llmStub.calls != 2 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, llmStub.calls)
	}
	if msgs[1].Metadata[MetaReminder] != "rule:end" {
		t.Errorf("history = %+v", msgs)
	}
}

// The dropped reply's usage rides on EventRuleInterrupt: the provider
// billed it, so adopters count it in the session cost.
func TestSteeringInterruptCarriesUsage(t *testing.T) {
	usage := &llm.TokenUsage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}
	llmStub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			callTool(cb, "c1", "bash", `{"command":"x"}`)
			cb(llm.StreamEvent{Type: llm.EventMessageDone, Usage: usage})
			return nil
		}
		sayText(cb, "done", nil)
		return nil
	}}
	l := &Loop{Client: llmStub, Tools: newRegistry(&fakeTool{name: "bash"}), Limits: DefaultLimits(), Steering: &fakeSteering{onCall: "bash"}}
	wait := collect(l)
	if _, _, err := l.Run(context.Background(), []Message{{Role: "user", Content: "x"}}); err != nil {
		t.Fatal(err)
	}
	for _, ev := range wait() {
		if ev.Kind == EventRuleInterrupt {
			if ev.Usage != usage {
				t.Errorf("interrupt usage = %+v", ev.Usage)
			}
			return
		}
	}
	t.Fatal("no EventRuleInterrupt")
}

// Past the turn's re-runs the calls run, so Steering is not asked about
// them: a reminder never says "do not run it" after it ran (review M1).
func TestSteeringCallsNotAskedPastTheCap(t *testing.T) {
	ran := 0
	bash := &fakeTool{name: "bash", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		ran++
		return tools.ToolResult{Content: "ok"}, nil
	}}
	llmStub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n <= DefaultMaxRuleInterrupts {
			callTool(cb, "c1", "bash", `{"command":"rm -rf x"}`)
			return nil
		}
		sayText(cb, "done", nil)
		return nil
	}}
	st := &fakeSteering{onCall: "bash"}
	l := &Loop{Client: llmStub, Tools: newRegistry(bash), Limits: DefaultLimits(), Steering: st}
	if _, _, err := l.Run(context.Background(), []Message{{Role: "user", Content: "x"}}); err != nil {
		t.Fatal(err)
	}
	if ran != 1 || st.callChecks != DefaultMaxRuleInterrupts {
		t.Errorf("ran=%d checks=%d, want 1 and %d", ran, st.callChecks, DefaultMaxRuleInterrupts)
	}
	if len(st.due[BoundaryRetry]) != 0 {
		t.Errorf("a reminder was queued for calls that ran: %+v", st.due)
	}
}
