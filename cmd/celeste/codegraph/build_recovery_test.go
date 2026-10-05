package codegraph

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recoveryFixture has non-Go edges that only pass 2 stores (a call across
// two Python files) next to the Go fixture, whose edges the Go pass stores.
func recoveryFixture() map[string]string {
	files := map[string]string{}
	for k, v := range goFixture {
		files[k] = v
	}
	files["py/a.py"] = "def caller():\n    helper()\n"
	files["py/b.py"] = "def helper():\n    pass\n"
	return files
}

// #388: a build interrupted between pass 1 (symbols and file records) and
// pass 2 (non-Go edges) left file records whose hashes match the files, so
// Update skipped them and their edges never came back. Update must notice
// the unfinished build and restore every edge.
func TestBuild_InterruptedBetweenPassesIsRepairedByUpdate(t *testing.T) {
	files := recoveryFixture()
	ref, _ := buildFixture(t, files)
	want := edgeKeys(t, ref)
	requireEdges(t, want, "caller -calls-> helper", fx+"b.UseT -calls-> ("+fx+"a.T).Update")

	ws := writeFixture(t, files)
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	// Two checks pass (BuildWithContext's entry and pass 1's first file);
	// the next one, between the passes, reports cancellation.
	err = idx.BuildWithContext(&failAfterCtx{Context: context.Background(), n: 2})
	require.ErrorIs(t, err, context.Canceled)
	got := edgeKeys(t, idx)
	require.False(t, got["caller -calls-> helper"], "the build must stop before pass 2")
	recs, err := idx.store.GetAllFiles()
	require.NoError(t, err)
	require.NotEmpty(t, recs, "pass 1 wrote file records whose hashes match")

	require.NoError(t, idx.Update())
	assert.Equal(t, want, edgeKeys(t, idx), "Update must restore every edge of the interrupted build")
	flag, err := idx.store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	assert.Nil(t, flag, "a finished build clears the in-progress mark")
}

// A build that finishes leaves no in-progress mark, so the next Update stays
// incremental instead of rebuilding.
func TestBuild_CompletedBuildClearsInProgressMark(t *testing.T) {
	idx, _ := buildFixture(t, recoveryFixture())
	for _, key := range []string{metaBuildInProgress, metaGoPassPending} {
		v, err := idx.store.GetMeta(key)
		require.NoError(t, err)
		assert.Nil(t, v, "%s must be cleared after a complete build", key)
	}
}

// An Update killed inside the Go pass (after a changed file's symbols and
// file record were stored, before the Go edges were rewritten) leaves the
// Go-pass mark set; the next Update reruns the Go pass even though every
// hash matches.
func TestUpdate_InterruptedGoPassIsRedone(t *testing.T) {
	idx, _ := buildFixture(t, recoveryFixture())
	want := edgeKeys(t, idx)

	// Simulate the kill: the mark is set and the Go edges are gone.
	require.NoError(t, idx.store.SetMeta(metaGoPassPending, []byte("1")))
	require.NoError(t, idx.store.ReplaceGoEdges(nil))
	require.False(t, edgeKeys(t, idx)[fx+"b.UseT -calls-> ("+fx+"a.T).Update"])

	require.NoError(t, idx.Update())
	assert.Equal(t, want, edgeKeys(t, idx))
	v, err := idx.store.GetMeta(metaGoPassPending)
	require.NoError(t, err)
	assert.Nil(t, v)
}
