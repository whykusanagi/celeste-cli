package agent

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePlanSteps(t *testing.T) {
	content := `1. Inspect files
2) Implement changes
- Run tests
* Write summary`

	steps := parsePlanSteps(content, 10)
	require.Len(t, steps, 4)
	assert.Equal(t, 1, steps[0].Index)
	assert.Equal(t, "Inspect files", steps[0].Title)
	assert.Equal(t, PlanStatusPending, steps[0].Status)
	assert.Equal(t, "Run tests", steps[2].Title)
}

func TestUpdatePlanProgressFromAssistantWithStepDoneMarker(t *testing.T) {
	state := NewRunState("goal", DefaultOptions())
	state.Plan = []PlanStep{
		{Index: 1, Title: "step one", Status: PlanStatusPending},
		{Index: 2, Title: "step two", Status: PlanStatusPending},
		{Index: 3, Title: "step three", Status: PlanStatusPending},
	}
	state.ActivePlanStep = 0

	updatePlanProgressFromAssistant(state, "Completed changes. STEP_DONE: 2", false)

	assert.Equal(t, PlanStatusCompleted, state.Plan[0].Status)
	assert.Equal(t, PlanStatusCompleted, state.Plan[1].Status)
	assert.Equal(t, PlanStatusInProgress, state.Plan[2].Status)
	assert.Equal(t, 2, state.ActivePlanStep)
}

func TestExecuteVerificationCommand(t *testing.T) {
	workspace := t.TempDir()

	pass := executeVerificationCommand(context.Background(), workspace, "printf ok", 2*time.Second)
	assert.True(t, pass.Passed)
	assert.Equal(t, 0, pass.ExitCode)
	assert.Contains(t, pass.Output, "ok")

	fail := executeVerificationCommand(context.Background(), workspace, "exit 2", 2*time.Second)
	assert.False(t, fail.Passed)
	assert.Equal(t, 2, fail.ExitCode)
}

func TestRunVerificationPhaseFailureReturnsToExecution(t *testing.T) {
	runner := &Runner{out: io.Discard, errOut: io.Discard}
	state := NewRunState("goal", DefaultOptions())
	state.Options.Workspace = t.TempDir()
	state.Options.RequireVerification = true
	state.Options.VerificationCommands = []string{"exit 1"}
	state.Options.VerifyTimeout = 2 * time.Second
	state.Options.CompletionMarker = "TASK_COMPLETE:"
	state.Status = StatusRunning
	state.Phase = PhaseExecution

	completed, err := runner.runVerificationPhase(context.Background(), state)
	require.NoError(t, err)
	assert.False(t, completed)
	assert.Equal(t, PhaseExecution, state.Phase)
	require.NotEmpty(t, state.Verification)
	assert.False(t, state.Verification[0].Passed)
	require.NotEmpty(t, state.Messages)
	last := state.Messages[len(state.Messages)-1]
	assert.Equal(t, "user", last.Role)
	assert.Contains(t, last.Content, "Verification failed")
}

func TestRunVerificationPhaseStopsAfterRetryLimit(t *testing.T) {
	runner := &Runner{out: io.Discard, errOut: io.Discard}
	state := NewRunState("goal", DefaultOptions())
	state.Options.Workspace = t.TempDir()
	state.Options.RequireVerification = true
	state.Options.VerificationCommands = []string{"exit 1"}
	state.Options.VerifyTimeout = 2 * time.Second
	state.Options.MaxVerificationRetries = 1
	state.Options.CompletionMarker = "TASK_COMPLETE:"
	state.Status = StatusRunning
	state.Phase = PhaseExecution

	completed, err := runner.runVerificationPhase(context.Background(), state)
	require.NoError(t, err)
	assert.True(t, completed)
	assert.Equal(t, StatusVerificationStop, state.Status)
	assert.Contains(t, state.Error, "verification failed after 1 attempt")
	assert.Equal(t, 1, state.VerificationAttempts)
}

func TestExtractBlockerMarker(t *testing.T) {
	assert.Equal(t, "", extractBlockerMarker("all good", "BLOCKED:"))
	assert.Equal(t, "missing API key", extractBlockerMarker("BLOCKED: missing API key", "BLOCKED:"))
	assert.Equal(t, "cannot reach endpoint", extractBlockerMarker("some text\nblocked: cannot reach endpoint\nmore", "BLOCKED:"))
}
