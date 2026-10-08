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
