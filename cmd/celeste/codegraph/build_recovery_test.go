package codegraph

import (
	"context"
	"fmt"
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
	// the next one, between the passes, reports cancellation. Pass 1 checks
	// ctx every 64 files, so this holds only while the fixture fits in one
	// stride; a larger fixture would cancel inside pass 1 instead.
	require.LessOrEqual(t, len(files), 64, "the fixture must fit in one pass-1 ctx stride")
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

// A full build clears a Go-pass mark left by an earlier killed pass even when
// the workspace no longer has Go files, so the next Update does not run an
// extra empty Go pass.
func TestBuild_ClearsGoPassMarkWithoutGoFiles(t *testing.T) {
	ws := writeFixture(t, map[string]string{
		"py/a.py": "def caller():\n    helper()\n",
		"py/b.py": "def helper():\n    pass\n",
	})
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.store.SetMeta(metaGoPassPending, []byte("1")))

	require.NoError(t, idx.Build())
	v, err := idx.store.GetMeta(metaGoPassPending)
	require.NoError(t, err)
	assert.Nil(t, v, "a complete build leaves no Go-pass mark")
}

// manyPyFiles is a Python workspace larger than one 64-file ctx stride,
// whose calls cross files, so its edges depend on every file's symbols.
func manyPyFiles(n int) map[string]string {
	files := map[string]string{}
	for i := 0; i < n; i++ {
		files[fmt.Sprintf("py/m%03d.py", i)] = fmt.Sprintf(
			"def caller_%d():\n    helper_%d()\n\ndef helper_%d():\n    pass\n", i, (i+1)%n, i)
	}
	return files
}

func graphCounts(t *testing.T, idx *Indexer) (files, symbols int) {
	t.Helper()
	st, err := idx.store.Stats()
	require.NoError(t, err)
	return st.TotalFiles, st.TotalSymbols
}

// #391: an Update that finds an unfinished build must not empty the graph
// and start over. Repeated short runs, each cancelled part-way (a user who
// keeps opening and closing celeste chat), keep the files the earlier runs
// indexed, index more, and the last one leaves the graph a clean build
// would.
func TestUpdate_RepeatedCancellationMakesProgressAndKeepsGraph(t *testing.T) {
	files := manyPyFiles(150)
	ref, _ := buildFixture(t, files)
	want := edgeKeys(t, ref)
	requireEdges(t, want, "caller_0 -calls-> helper_1", "caller_149 -calls-> helper_0")

	ws := writeFixture(t, files)
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	// The build stops inside pass 1, after its first stride of 64 files.
	err = idx.BuildWithContext(&failAfterCtx{Context: context.Background(), n: 2})
	require.ErrorIs(t, err, context.Canceled)
	prevFiles, prevSyms := graphCounts(t, idx)
	require.Positive(t, prevFiles)

	finished := false
	for run := 0; run < 10 && !finished; run++ {
		// Three checks pass: the entry, the scan's first file and the
		// first re-indexed file; the next one cancels the run.
		err := idx.UpdateWithContext(&failAfterCtx{Context: context.Background(), n: 3})
		nFiles, nSyms := graphCounts(t, idx)
		require.GreaterOrEqual(t, nFiles, prevFiles, "run %d dropped indexed files", run)
		require.GreaterOrEqual(t, nSyms, prevSyms, "run %d dropped symbols", run)
		if err == nil {
			finished = true
			break
		}
		require.ErrorIs(t, err, context.Canceled)
		mark, gerr := idx.store.GetMeta(metaBuildInProgress)
		require.NoError(t, gerr)
		require.NotNil(t, mark, "a cancelled recovery keeps the mark")
		prevFiles, prevSyms = nFiles, nSyms
	}
	require.True(t, finished, "repeated short runs must finish the index")
	assert.Equal(t, want, edgeKeys(t, idx))
	mark, err := idx.store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	assert.Nil(t, mark)
}
