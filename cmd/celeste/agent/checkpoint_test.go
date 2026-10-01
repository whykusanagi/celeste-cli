package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func TestCheckpointLoadRejectsPathTraversal(t *testing.T) {
	store, err := NewCheckpointStore(t.TempDir())
	require.NoError(t, err)

	_, err = store.Load("../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid run id")

	_, err = store.Load("../sibling-dir/sneaky")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid run id")
}

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

// An agent run checkpointed before 2.0 may hold an uncapped tool result;
// resuming it must not send the whole thing (2.0 F3, ruling 9).
func TestCheckpointLoadCapsOversizedToolResults(t *testing.T) {
	store, err := NewCheckpointStore(t.TempDir())
	require.NoError(t, err)
	huge := strings.Repeat("y", 300*1024)
	require.NoError(t, store.Save(&RunState{RunID: "legacy", Messages: []tui.ChatMessage{
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "c1", Name: "bash"}}},
		{Role: "tool", ToolCallID: "c1", Name: "bash", Content: huge},
	}}))
	got, err := store.Load("legacy")
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
	assert.LessOrEqual(t, len(got.Messages[2].Content), ctxmgr.DefaultMaxToolResultBytes)
	assert.Equal(t, "go", got.Messages[0].Content)
}
