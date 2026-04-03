package planning

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlanMode(t *testing.T) {
	pm := NewPlanMode()
	assert.False(t, pm.IsActive())
	assert.Empty(t, pm.PlanPath)
	assert.Nil(t, pm.Steps)
}

func TestEnterAndIsActive(t *testing.T) {
	pm := NewPlanMode()
	pm.Enter("/tmp/plan.md")
	assert.True(t, pm.IsActive())
	assert.Equal(t, "/tmp/plan.md", pm.PlanPath)
}

func TestWriteAndReadPlan(t *testing.T) {
	tmp := t.TempDir()
	planPath := filepath.Join(tmp, "plan.md")

	pm := NewPlanMode()
	pm.Enter(planPath)

	content := "- [ ] Step one\n- [ ] Step two\n"
	require.NoError(t, pm.WritePlan(content))

	read, err := pm.ReadPlan()
	require.NoError(t, err)
	assert.Equal(t, content, read)
}

func TestWritePlanNoPath(t *testing.T) {
	pm := NewPlanMode()
	err := pm.WritePlan("hello")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no plan path")
}

func TestReadPlanNoPath(t *testing.T) {
	pm := NewPlanMode()
	_, err := pm.ReadPlan()
	assert.Error(t, err)
}

func TestExitReadsPlanFromDisk(t *testing.T) {
	tmp := t.TempDir()
	planPath := filepath.Join(tmp, "plan.md")

	pm := NewPlanMode()
	pm.Enter(planPath)

	content := "- [ ] First task\n- [x] Second task\n- [~] Third task\n- [>] Fourth task\n"
	require.NoError(t, os.WriteFile(planPath, []byte(content), 0644))

	snapshot, err := pm.Exit()
	require.NoError(t, err)
	assert.False(t, pm.IsActive())

	require.Len(t, snapshot.Steps, 4)
	assert.Equal(t, "First task", snapshot.Steps[0].Description)
	assert.Equal(t, "pending", snapshot.Steps[0].Status)
	assert.Equal(t, "Second task", snapshot.Steps[1].Description)
	assert.Equal(t, "done", snapshot.Steps[1].Status)
	assert.Equal(t, "Third task", snapshot.Steps[2].Description)
	assert.Equal(t, "skipped", snapshot.Steps[2].Status)
	assert.Equal(t, "Fourth task", snapshot.Steps[3].Description)
	assert.Equal(t, "in_progress", snapshot.Steps[3].Status)
}

func TestExitWhenNotActive(t *testing.T) {
	pm := NewPlanMode()
	_, err := pm.Exit()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not active")
}

func TestExitWithMissingFile(t *testing.T) {
	pm := NewPlanMode()
	pm.Enter("/tmp/nonexistent-plan-12345.md")
	_, err := pm.Exit()
	assert.Error(t, err)
	assert.False(t, pm.IsActive()) // Still deactivates
}

func TestParseSteps(t *testing.T) {
	content := `# My Plan

Some intro text.

- [ ] Do the first thing
- [x] Already done
- [X] Also done (uppercase)
- [~] Skipped this
- [>] Currently working
- Not a checklist item
- [ ]
`
	steps := ParseSteps(content)
	require.Len(t, steps, 5)
	assert.Equal(t, "pending", steps[0].Status)
	assert.Equal(t, "done", steps[1].Status)
	assert.Equal(t, "done", steps[2].Status)
	assert.Equal(t, "skipped", steps[3].Status)
	assert.Equal(t, "in_progress", steps[4].Status)
}

func TestExitReturnsSnapshot(t *testing.T) {
	tmp := t.TempDir()
	planPath := filepath.Join(tmp, "plan.md")

	pm := NewPlanMode()
	pm.Enter(planPath)
	require.NoError(t, os.WriteFile(planPath, []byte("- [ ] Task\n"), 0644))

	snapshot, err := pm.Exit()
	require.NoError(t, err)

	// Snapshot path should be preserved.
	assert.Equal(t, planPath, snapshot.PlanPath)
	// Snapshot should be independent of pm.
	assert.False(t, snapshot.Active)
}
