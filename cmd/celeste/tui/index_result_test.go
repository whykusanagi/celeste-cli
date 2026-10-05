package tui

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// #392 review: /index update or rebuild finding another indexer at work
// shows a notice, not "update failed".
func TestIndexRunResult(t *testing.T) {
	idx, err := codegraph.NewIndexer(t.TempDir(), filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	busy := indexRunResult("update", "updated", codegraph.ErrIndexBusy, idx)
	msg, ok := busy.(AgentProgressMsg)
	require.True(t, ok, "busy is a notice, got %T", busy)
	assert.Contains(t, msg.Text, "another celeste process")

	ok2 := indexRunResult("update", "updated", nil, idx)
	assert.Contains(t, ok2.(AgentProgressMsg).Text, "Index updated: 0 files")

	failed := indexRunResult("rebuild", "rebuilt", errors.New("disk full"), idx)
	sem, isErr := failed.(StreamErrorMsg)
	require.True(t, isErr)
	assert.Contains(t, sem.Err.Error(), "rebuild failed: disk full")
}
