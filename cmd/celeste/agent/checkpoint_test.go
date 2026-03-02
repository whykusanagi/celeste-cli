package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckpointSaveLoadAndList(t *testing.T) {
	store, err := NewCheckpointStore(t.TempDir())
	require.NoError(t, err)

	state := NewRunState("test goal", DefaultOptions())
	state.Status = StatusRunning
	state.Turn = 2
	state.ToolCallCount = 3

	err = store.Save(state)
	require.NoError(t, err)

	loaded, err := store.Load(state.RunID)
	require.NoError(t, err)
	assert.Equal(t, state.RunID, loaded.RunID)
	assert.Equal(t, "test goal", loaded.Goal)
	assert.Equal(t, 2, loaded.Turn)
	assert.Equal(t, 3, loaded.ToolCallCount)

	state2 := NewRunState("newer goal", DefaultOptions())
	state2.UpdatedAt = time.Now().Add(1 * time.Minute)
	err = store.Save(state2)
	require.NoError(t, err)

	summaries, err := store.List(10)
	require.NoError(t, err)
	require.Len(t, summaries, 2)
	assert.Equal(t, state2.RunID, summaries[0].RunID)
}
