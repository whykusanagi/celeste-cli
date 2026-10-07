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
	tracker.RecordUsage("grok-4-1-fast", Usage{Input: 1000, Output: 500})

	s := tracker.GetSummary()
	assert.Equal(t, "grok-4-1-fast", s.Model)
	assert.Equal(t, 1000, s.TotalInput)
	assert.Equal(t, 500, s.TotalOutput)
	assert.Equal(t, 1, s.Turns)
	assert.True(t, s.TotalCostUSD > 0)
}

func TestSessionTracker_MultipleTurns(t *testing.T) {
	tracker := NewSessionTracker()
	tracker.RecordUsage("grok-4-1-fast", Usage{Input: 1000, Output: 500})
	tracker.RecordUsage("grok-4-1-fast", Usage{Input: 2000, Output: 1000})

	s := tracker.GetSummary()
	assert.Equal(t, 3000, s.TotalInput)
	assert.Equal(t, 1500, s.TotalOutput)
	assert.Equal(t, 2, s.Turns)
}

func TestSessionTracker_SaveLoad(t *testing.T) {
	tracker := NewSessionTracker()
	tracker.RecordUsage("claude-sonnet-4", Usage{Input: 5000, Output: 2000})

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
