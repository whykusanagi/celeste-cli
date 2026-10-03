package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
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

// Agent checkpoints marshal tui.ChatMessage directly; blocks survive Save /
// Load byte for byte, and messages without blocks write no key.
func TestCheckpointRoundTripsProviderBlocks(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCheckpointStore(dir)
	require.NoError(t, err)
	pb, err := tui.NewProviderBlocks("k", []json.RawMessage{json.RawMessage(`{"type":"thinking","thinking":"x < y","signature":"S"}`)})
	require.NoError(t, err)
	require.NoError(t, store.Save(&RunState{RunID: "r1", Messages: []tui.ChatMessage{
		{Role: "user", Content: "go"},
		tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant", Content: "done"}, pb),
	}}))
	data, err := os.ReadFile(filepath.Join(dir, "agent", "runs", "r1.json"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(data), `"provider_blocks"`))
	got, err := store.Load("r1")
	require.NoError(t, err)
	blocks, ok := tui.ReplayBlocks(got.Messages[1], "k")
	require.True(t, ok)
	assert.Equal(t, string(pb.Blocks[0]), string(blocks[0]))
}

// A damaged provider_blocks value loads as the zero value (it never fails
// the load) and is not written back; neither are blocks that no longer
// match their message (2.0 F3, API review).
func TestCheckpointSaveDropsZeroAndStaleBlocks(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCheckpointStore(dir)
	require.NoError(t, err)
	path := filepath.Join(dir, "agent", "runs", "r2.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"run_id":"r2","messages":[
		{"Role":"user","Content":"go"},
		{"Role":"assistant","Content":"done","provider_blocks":{"provider":"k","blocks":5}}]}`), 0o644))
	got, err := store.Load("r2")
	require.NoError(t, err, "a damaged provider_blocks never fails the load")
	require.NotNil(t, got.Messages[1].ProviderBlocks)
	pb, err := tui.NewProviderBlocks("k", []json.RawMessage{json.RawMessage(`{"a":1}`)})
	require.NoError(t, err)
	stale := tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant", Content: "x"}, pb)
	stale.Content = "edited"
	got.Messages = append(got.Messages, stale)
	shared := got.Messages
	require.NoError(t, store.Save(got))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "provider_blocks")
	assert.NotNil(t, shared[2].ProviderBlocks, "Save does not modify a history slice others may hold")
}
