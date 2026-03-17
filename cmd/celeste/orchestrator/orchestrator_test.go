package orchestrator_test

import (
	"context"
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
