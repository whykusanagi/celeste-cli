package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
)

// fakeRunner satisfies the orchestrator.AgentRunner interface for tests.
type fakeRunner struct {
	response string
	err      error
}

func (f *fakeRunner) RunGoal(_ context.Context, _ string) (string, error) {
	return f.response, f.err
}

func TestOrchestratorClassifiesAndRoutesGoal(t *testing.T) {
	cfg := &config.Config{Model: "test-model"}
	events := []orchestrator.OrchestratorEvent{}

	o := orchestrator.New(cfg, orchestrator.WithRunnerFactory(func(model string) orchestrator.AgentRunner {
		return &fakeRunner{response: "TASK_COMPLETE: done"}
	}))
	o.OnEvent(func(e orchestrator.OrchestratorEvent) {
		events = append(events, e)
	})

	result, err := o.Run(context.Background(), "fix the broken test in auth.go")
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Must have emitted a classification event
	var classified bool
	for _, e := range events {
		if e.Kind == orchestrator.EventClassified {
			classified = true
			assert.Equal(t, orchestrator.LaneCode, e.Lane)
		}
	}
	assert.True(t, classified, "expected EventClassified to be emitted")

	// Must have emitted a complete event
	var completed bool
	for _, e := range events {
		if e.Kind == orchestrator.EventComplete {
			completed = true
		}
	}
	assert.True(t, completed)
}

// A failed debate is non-fatal: "debate skipped" is a notice (EventAction),
// not EventError, which callers such as the TUI treat as the end of the run.
// The run still ends with EventComplete and the primary's output.
func TestOrchestratorSkippedDebateIsNotTerminal(t *testing.T) {
	cfg := &config.Config{Model: "p", Orchestrator: &config.OrchestratorConfig{Lanes: map[string]config.LaneConfig{
		"code": {Primary: "p", Reviewer: "r"},
	}}}
	o := orchestrator.New(cfg, orchestrator.WithRunnerFactory(func(model string) orchestrator.AgentRunner {
		if model == "r" {
			return &fakeRunner{err: errors.New("reviewer down")}
		}
		return &fakeRunner{response: "TASK_COMPLETE: fixed"}
	}))
	var events []orchestrator.OrchestratorEvent
	o.OnEvent(func(e orchestrator.OrchestratorEvent) { events = append(events, e) })

	res, err := o.Run(context.Background(), "fix the bug in main.go")
	require.NoError(t, err)
	assert.Nil(t, res.Verdict)
	skipped := false
	for _, e := range events {
		assert.NotEqual(t, orchestrator.EventError, e.Kind, "event %q", e.Text)
		if strings.Contains(e.Text, "debate skipped") && strings.Contains(e.Text, "reviewer down") {
			skipped = true
			assert.Equal(t, orchestrator.EventAction, e.Kind)
		}
	}
	assert.True(t, skipped, "no debate-skipped notice in %v", events)
	require.NotEmpty(t, events)
	last := events[len(events)-1]
	assert.Equal(t, orchestrator.EventComplete, last.Kind)
	assert.Equal(t, "TASK_COMPLETE: fixed", last.Text)
}
