package subagents

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// A kill that lands after the agent finished but before its result is
// recorded wins: the run stays failed ("killed by user"), so an isolated
// run's worktree is not merged into the parent repository.
func TestKillBeforeFinishIsNotOverwritten(t *testing.T) {
	m := NewManager(&config.Config{}, t.TempDir(), false)
	run := &SubagentRun{ID: "sub-1", Element: "fire", Status: "running"}
	m.mu.Lock()
	m.runs[run.ID] = run
	m.cancels[run.ID] = func() {}
	m.mu.Unlock()

	if !m.Kill(run.ID) {
		t.Fatal("Kill returned false")
	}
	// RunGoal already returned success when the kill landed.
	state := &agent.RunState{Status: agent.StatusCompleted, Turn: 3, LastAssistantResponse: "done"}
	if err := m.finishRun(run, state, nil, nil, ""); err == nil {
		t.Fatal("finishRun reported success for a killed run")
	}
	m.mu.Lock()
	status, why := run.Status, run.Error
	m.mu.Unlock()
	if status != "failed" || why != "killed by user" {
		t.Fatalf("status = %q (%q), want failed (killed by user)", status, why)
	}
	if m.mergeable(run) {
		t.Fatal("a killed run's worktree would be merged")
	}
}

// A kill that lands after finishRun recorded "completed" but before the
// deferred merge still stops the merge: Kill reports the kill, so the run
// must end failed and unmergeable.
func TestKillAfterCompletionStopsMerge(t *testing.T) {
	m := NewManager(&config.Config{}, t.TempDir(), false)
	run := &SubagentRun{ID: "sub-2", Element: "water", Status: "running"}
	m.mu.Lock()
	m.runs[run.ID] = run
	m.cancels[run.ID] = func() {}
	m.mu.Unlock()

	state := &agent.RunState{Status: agent.StatusCompleted, Turn: 2, LastAssistantResponse: "done"}
	if err := m.finishRun(run, state, nil, nil, ""); err != nil {
		t.Fatalf("finishRun: %v", err)
	}
	if !m.Kill(run.ID) {
		t.Fatal("Kill returned false while the run's cancel was registered")
	}
	if m.mergeable(run) {
		t.Fatal("a run killed before its merge would still be merged")
	}
	m.mu.Lock()
	status, why := run.Status, run.Error
	m.mu.Unlock()
	if status != "failed" || why != "killed by user" {
		t.Fatalf("status = %q (%q), want failed (killed by user)", status, why)
	}
}

// Once the deferred merge has decided to merge a completed run, a kill
// can no longer report it killed (Aikido, #425): the decision and the
// kill are serialized, so the run that is merged stays completed and Kill
// reports nothing to kill, instead of a "killed" run whose changes merge.
func TestKillAfterTheMergeDecisionIsRefused(t *testing.T) {
	m := NewManager(&config.Config{}, t.TempDir(), false)
	run := &SubagentRun{ID: "sub-3", Element: "earth", Status: "running"}
	m.mu.Lock()
	m.runs[run.ID] = run
	m.cancels[run.ID] = func() {}
	m.mu.Unlock()

	state := &agent.RunState{Status: agent.StatusCompleted, Turn: 2, LastAssistantResponse: "done"}
	if err := m.finishRun(run, state, nil, nil, ""); err != nil {
		t.Fatalf("finishRun: %v", err)
	}
	if !m.mergeable(run) {
		t.Fatal("a completed run was not mergeable")
	}
	// The merge is now under way: a kill in this window must not succeed.
	if m.Kill(run.ID) {
		t.Fatal("Kill reported a kill for a run whose merge was already decided")
	}
	m.mu.Lock()
	status, killed := run.Status, run.killed
	m.mu.Unlock()
	if status != "completed" || killed {
		t.Fatalf("status = %q, killed = %v; want completed and not killed", status, killed)
	}
}
