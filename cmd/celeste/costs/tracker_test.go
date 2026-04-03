package costs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionTracker_RecordUsage(t *testing.T) {
	tracker := NewSessionTracker()
	tracker.RecordUsage("gpt-4o", 1000, 500)

	s := tracker.GetSummary()
	assert.Equal(t, "gpt-4o", s.Model)
	assert.Equal(t, 1000, s.TotalInput)
	assert.Equal(t, 500, s.TotalOutput)
	assert.Equal(t, 1, s.Turns)
	assert.True(t, s.TotalCostUSD > 0)
}

func TestSessionTracker_MultipleTurns(t *testing.T) {
	tracker := NewSessionTracker()
	tracker.RecordUsage("gpt-4o", 1000, 500)
	tracker.RecordUsage("gpt-4o", 2000, 1000)

	s := tracker.GetSummary()
	assert.Equal(t, 3000, s.TotalInput)
	assert.Equal(t, 1500, s.TotalOutput)
	assert.Equal(t, 2, s.Turns)
}

func TestSessionTracker_SaveLoad(t *testing.T) {
	tracker := NewSessionTracker()
	tracker.RecordUsage("claude-sonnet-4", 5000, 2000)

	path := filepath.Join(t.TempDir(), "cost.json")
	require.NoError(t, tracker.Save(path))

	// Verify file exists
	_, err := os.Stat(path)
	require.NoError(t, err)

	loaded := NewSessionTracker()
	require.NoError(t, loaded.Load(path))

	assert.Equal(t, tracker.GetSummary(), loaded.GetSummary())
}

func TestSessionTracker_LoadNotFound(t *testing.T) {
	tracker := NewSessionTracker()
	err := tracker.Load("/nonexistent/path.json")
	assert.Error(t, err)
}
