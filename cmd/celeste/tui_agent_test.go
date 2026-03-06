package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

type fakeAgentRunner struct {
	listRunsFn func(limit int) ([]agent.RunSummary, error)
	resumeFn   func(ctx context.Context, runID string) (*agent.RunState, error)
	runGoalFn  func(ctx context.Context, goal string) (*agent.RunState, error)
	sink       agent.EventSink
}

func (f *fakeAgentRunner) ListRuns(limit int) ([]agent.RunSummary, error) {
	if f.listRunsFn != nil {
		return f.listRunsFn(limit)
	}
	return nil, nil
}

func (f *fakeAgentRunner) Resume(ctx context.Context, runID string) (*agent.RunState, error) {
	if f.resumeFn != nil {
		return f.resumeFn(ctx, runID)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeAgentRunner) RunGoal(ctx context.Context, goal string) (*agent.RunState, error) {
	if f.runGoalFn != nil {
		return f.runGoalFn(ctx, goal)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeAgentRunner) SetEventSink(sink agent.EventSink) {
	f.sink = sink
}

func TestExecuteAgentCommandListRuns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := agent.NewCheckpointStore("")
	require.NoError(t, err)

	state := agent.NewRunState("fix tests", agent.DefaultOptions())
	state.Status = agent.StatusCompleted
	state.Turn = 3
	state.ToolCallCount = 2
	state.UpdatedAt = time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)
	require.NoError(t, store.Save(state))

	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{
			APIKey:  "test-key",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4o-mini",
		},
	}

	output, err := adapter.executeAgentCommand([]string{"list-runs"})
	require.NoError(t, err)
	assert.Contains(t, output, "Recent Agent Runs (1):")
	assert.Contains(t, output, state.RunID)
}

func TestExecuteAgentCommandGoal(t *testing.T) {
	originalFactory := newAgentRunnerForTUI
	t.Cleanup(func() { newAgentRunnerForTUI = originalFactory })

	newAgentRunnerForTUI = func(cfg *config.Config, options agent.Options, out io.Writer, errOut io.Writer) (agentRunnerAPI, error) {
		return &fakeAgentRunner{
			runGoalFn: func(ctx context.Context, goal string) (*agent.RunState, error) {
				require.Equal(t, "build release notes", goal)
				return &agent.RunState{
					RunID:                 "run-456",
					Status:                agent.StatusCompleted,
					Turn:                  2,
					ToolCallCount:         1,
					LastAssistantResponse: "TASK_COMPLETE: done",
				}, nil
			},
		}, nil
	}

	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{
			APIKey:  "test-key",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4o-mini",
		},
	}

	output, err := adapter.executeAgentCommand([]string{"build", "release", "notes"})
	require.NoError(t, err)
	assert.Contains(t, output, "Run ID: run-456")
	assert.Contains(t, output, "Status: completed")
	assert.Contains(t, output, "Final Response:")
}

func TestExecuteAgentCommandRequiresCredentials(t *testing.T) {
	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{
			APIKey:  "",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4o-mini",
		},
	}

	output, err := adapter.executeAgentCommand([]string{"implement", "phase", "five"})
	require.Error(t, err)
	assert.Equal(t, "", strings.TrimSpace(output))
	assert.Contains(t, err.Error(), "no API key or Google credentials configured")
}

func TestExecuteAgentCommandStopNoActiveRun(t *testing.T) {
	adapter := &TUIClientAdapter{
		agentEvents: make(chan tui.AgentEventMsg, 1),
	}

	output, err := adapter.executeAgentCommand([]string{"stop"})
	require.Error(t, err)
	assert.Contains(t, output, "No active agent run")
}

func TestExecuteAgentCommandShow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := agent.NewCheckpointStore("")
	require.NoError(t, err)

	state := agent.NewRunState("publish docs", agent.DefaultOptions())
	state.Status = agent.StatusCompleted
	state.Turn = 2
	state.ToolCallCount = 1
	state.LastAssistantResponse = "TASK_COMPLETE: docs published"
	require.NoError(t, store.Save(state))

	adapter := &TUIClientAdapter{}
	output, err := adapter.executeAgentCommand([]string{"show", state.RunID})
	require.NoError(t, err)
	assert.Contains(t, output, "Run ID: "+state.RunID)
	assert.Contains(t, output, "Status: completed")
}
