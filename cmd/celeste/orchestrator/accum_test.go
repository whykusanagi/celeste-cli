package orchestrator

import (
	"context"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// lateRunner emits two events during RunGoal and more from a goroutine it
// does not wait for, the way an event from another goroutine can reach the
// orchestrator after RunGoal returns.
type lateRunner struct {
	emit func(OrchestratorEvent)
	late chan struct{}
}

func (r *lateRunner) RunGoal(context.Context, string) (string, error) {
	r.emit(OrchestratorEvent{Kind: EventAction, InputTokens: 10, OutputTokens: 5})
	r.emit(OrchestratorEvent{Kind: EventAction, InputTokens: 10, OutputTokens: 5})
	go func() {
		defer close(r.late)
		for i := 0; i < 50; i++ {
			r.emit(OrchestratorEvent{Kind: EventAction, Text: "late"})
		}
	}()
	return "out", nil
}

// runGoalAccumStats used to replace o.onEvent for the run and restore it
// after, unsynchronized with events from other goroutines (F2a ledger,
// Task 9 carry). Events now go through emit under a mutex: totals count the
// run's events, emit delivers every event to the current callback (dropping
// late ones is the lane runner's gate, not emit's), and replacing the
// callback mid-stream is race-free (run with -race).
func TestRunGoalAccumStatsDoesNotSwapOnEvent(t *testing.T) {
	o := New(&config.Config{Model: "m"})
	var mu sync.Mutex
	got := 0
	count := func(OrchestratorEvent) { mu.Lock(); got++; mu.Unlock() }
	o.OnEvent(count)
	r := &lateRunner{emit: o.emit, late: make(chan struct{})}

	out, _, in, outTok, err := o.runGoalAccumStats(context.Background(), r, "goal")
	o.OnEvent(count) // replace the callback while late events are still arriving
	<-r.late

	if err != nil || out != "out" || in != 20 || outTok != 10 {
		t.Fatalf("out=%q in=%d out=%d err=%v, want the run's two events counted", out, in, outTok, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got != 52 {
		t.Fatalf("callback saw %d events, want all 52", got)
	}
}
