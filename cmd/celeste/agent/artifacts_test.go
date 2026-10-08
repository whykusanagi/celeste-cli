package agent

import (
	"os"
	"path/filepath"
	"runtime"
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

// Aikido 806869790: a bundle holds the run's transcript and diff:
// owner-only directory and files.
func TestWriteArtifactBundleIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	state := NewRunState("test goal", DefaultOptions())
	state.Options.EmitArtifacts = true
	state.Options.ArtifactDir = t.TempDir()
	bundlePath, err := writeArtifactBundle(state)
	require.NoError(t, err)
	fi, err := os.Stat(bundlePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), fi.Mode().Perm(), "bundle dir")
	entries, err := os.ReadDir(bundlePath)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		fi, err := os.Stat(filepath.Join(bundlePath, e.Name()))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), e.Name())
	}
}
