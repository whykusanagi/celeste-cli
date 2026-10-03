package orchestrator

import (
	"context"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
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
	o := &orchRun{Orchestrator: New(&config.Config{Model: "m"})}
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

// tokenRunner emits one event carrying n input tokens, then waits for
// release, so two runs' lanes overlap.
type tokenRunner struct {
	emit    func(OrchestratorEvent)
	n       int
	emitted chan struct{}
	release chan struct{}
}

func (r *tokenRunner) RunGoal(context.Context, string) (string, error) {
	r.emit(OrchestratorEvent{Kind: EventAction, InputTokens: r.n})
	close(r.emitted)
	<-r.release
	return "", nil
}

// Two Runs of one Orchestrator in flight at once each total only their own
// lanes' tokens (the accumulators used to live on the Orchestrator, shared
// by every Run, so a run counted the other's tokens too).
func TestConcurrentRunsCountOnlyTheirOwnTokens(t *testing.T) {
	orch := New(&config.Config{Model: "m"})
	ra, rb := &orchRun{Orchestrator: orch}, &orchRun{Orchestrator: orch}
	a := &tokenRunner{emit: ra.emit, n: 10, emitted: make(chan struct{}), release: make(chan struct{})}
	b := &tokenRunner{emit: rb.emit, n: 100, emitted: make(chan struct{}), release: make(chan struct{})}
	var wg sync.WaitGroup
	var inA, inB int
	wg.Add(2)
	go func() { defer wg.Done(); _, _, inA, _, _ = ra.runGoalAccumStats(context.Background(), a, "a") }()
	<-a.emitted
	go func() { defer wg.Done(); _, _, inB, _, _ = rb.runGoalAccumStats(context.Background(), b, "b") }()
	<-b.emitted
	close(a.release)
	close(b.release)
	wg.Wait()
	if inA != 10 || inB != 100 {
		t.Fatalf("run A counted %d tokens, run B %d; want 10 and 100", inA, inB)
	}
}
