package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteArtifactBundle(t *testing.T) {
	state := NewRunState("test goal", DefaultOptions())
	state.Options.EmitArtifacts = true
	state.Options.ArtifactDir = t.TempDir()
	state.Status = StatusCompleted
	state.Phase = PhaseExecution
	state.LastAssistantResponse = "TASK_COMPLETE: done"
	state.Plan = []PlanStep{{Index: 1, Title: "step", Status: PlanStatusCompleted}}

	bundlePath, err := writeArtifactBundle(state)
	require.NoError(t, err)

	require.DirExists(t, bundlePath)
	assert.FileExists(t, filepath.Join(bundlePath, "summary.md"))
	assert.FileExists(t, filepath.Join(bundlePath, "run_state.json"))
	assert.FileExists(t, filepath.Join(bundlePath, "plan.json"))
	assert.FileExists(t, filepath.Join(bundlePath, "steps.json"))
	assert.FileExists(t, filepath.Join(bundlePath, "verification.json"))

	summaryData, err := os.ReadFile(filepath.Join(bundlePath, "summary.md"))
	require.NoError(t, err)
	assert.Contains(t, string(summaryData), "Agent Run Summary")
	assert.Contains(t, string(summaryData), "TASK_COMPLETE")
}
