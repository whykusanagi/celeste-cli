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
)

type fakeAgentRunner struct {
	listRunsFn func(limit int) ([]agent.RunSummary, error)
	resumeFn   func(ctx context.Context, runID string) (*agent.RunState, error)
	runGoalFn  func(ctx context.Context, goal string) (*agent.RunState, error)
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

func TestExecuteAgentCommandListRuns(t *testing.T) {
	originalFactory := newAgentRunnerForTUI
	t.Cleanup(func() { newAgentRunnerForTUI = originalFactory })

	newAgentRunnerForTUI = func(cfg *config.Config, options agent.Options, out io.Writer, errOut io.Writer) (agentRunnerAPI, error) {
		return &fakeAgentRunner{
			listRunsFn: func(limit int) ([]agent.RunSummary, error) {
				require.Equal(t, 20, limit)
				return []agent.RunSummary{
					{
						RunID:     "run-123",
						Goal:      "fix tests",
						Status:    agent.StatusCompleted,
						UpdatedAt: time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC),
						Turn:      3,
						ToolCalls: 2,
					},
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

	output, err := adapter.executeAgentCommand([]string{"list-runs"})
	require.NoError(t, err)
	assert.Contains(t, output, "Recent Agent Runs (1):")
	assert.Contains(t, output, "run-123")
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

	output, err := adapter.executeAgentCommand([]string{"list-runs"})
	require.Error(t, err)
	assert.Equal(t, "", strings.TrimSpace(output))
	assert.Contains(t, err.Error(), "no API key or Google credentials configured")
}
