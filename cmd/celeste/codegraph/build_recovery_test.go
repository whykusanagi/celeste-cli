package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
// indexed and index more. Re-resolving the non-Go edges is not incremental
// (it stops when ctx ends, so Env.Close does not wait for it), so the run
// that finishes is one that outlasts it; that run leaves the graph a clean
// build would.
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

	for run := 0; run < 10 && prevFiles < len(files); run++ {
		// Three checks pass: the entry, the scan's first file and the
		// first re-indexed file; the next one cancels the run.
		err := idx.UpdateWithContext(&failAfterCtx{Context: context.Background(), n: 3})
		require.ErrorIs(t, err, context.Canceled)
		nFiles, nSyms := graphCounts(t, idx)
		require.GreaterOrEqual(t, nFiles, prevFiles, "run %d dropped indexed files", run)
		require.GreaterOrEqual(t, nSyms, prevSyms, "run %d dropped symbols", run)
		mark, gerr := idx.store.GetMeta(metaBuildInProgress)
		require.NoError(t, gerr)
		require.NotNil(t, mark, "a cancelled recovery keeps the mark")
		prevFiles, prevSyms = nFiles, nSyms
	}
	require.Equal(t, len(files), prevFiles, "repeated short runs must index every file")

	require.NoError(t, idx.Update())
	assert.Equal(t, want, edgeKeys(t, idx))
	mark, err := idx.store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	assert.Nil(t, mark)
}

// #391 review: Env.Close cancels the index context and waits for the run,
// so a recovering update must stop re-resolving non-Go edges when ctx ends
// instead of parsing the whole repo first. A cancel while parsing must not
// delete any edge, and the mark stays so the next run finishes the work.
func TestUpdate_RecoveryReresolveStopsOnCancel(t *testing.T) {
	files := manyPyFiles(400)
	idx, _ := buildFixture(t, files)
	want := edgeKeys(t, idx)
	require.NoError(t, idx.store.SetMeta(metaBuildInProgress, []byte("killed-run")))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	parsed := 0
	testHookReresolveParse = func(string) {
		parsed++
		if parsed == 100 {
			cancel()
		}
	}
	t.Cleanup(func() { testHookReresolveParse = nil })

	start := time.Now()
	err := idx.UpdateWithContext(ctx)
	elapsed := time.Since(start)
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, parsed, 200, "the re-resolve must stop within a stride of the cancel")
	assert.Less(t, elapsed, 5*time.Second)
	assert.Equal(t, want, edgeKeys(t, idx), "a cancel while parsing deletes no edge")
	mark, err := idx.store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	assert.NotNil(t, mark, "a cancelled recovery keeps the mark")

	testHookReresolveParse = nil
	require.NoError(t, idx.Update())
	assert.Equal(t, want, edgeKeys(t, idx))
	mark, err = idx.store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	assert.Nil(t, mark)
}

// A recovering update re-parses for the re-resolve only the files it did
// not re-index itself in the same run.
func TestUpdate_RecoveryReusesEdgesOfFilesItIndexed(t *testing.T) {
	files := manyPyFiles(100)
	ref, _ := buildFixture(t, files)
	want := edgeKeys(t, ref)

	ws := writeFixture(t, files)
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	// The build stops inside pass 1, after its first stride of 64 files.
	err = idx.BuildWithContext(&failAfterCtx{Context: context.Background(), n: 2})
	require.ErrorIs(t, err, context.Canceled)
	indexed, _ := graphCounts(t, idx)
	require.Less(t, indexed, len(files))

	parsed := 0
	testHookReresolveParse = func(string) { parsed++ }
	t.Cleanup(func() { testHookReresolveParse = nil })
	require.NoError(t, idx.Update())
	assert.Equal(t, indexed, parsed, "only the files indexed by earlier runs are parsed again")
	assert.Equal(t, want, edgeKeys(t, idx))
}

// #391 review: a clean build resolves non-Go edges before any Go symbol is
// stored, so a Python call never lands on a same-named Go function. A
// recovering update (and an incremental one) re-resolves them with the Go
// symbols present and must not do so either.
func TestUpdate_NonGoCallsNeverResolveToGo(t *testing.T) {
	files := map[string]string{
		"go.mod":  "module example.com/x\n\ngo 1.22\n",
		"x.go":    "package x\n\nfunc helper() {}\n",
		"py/a.py": "def caller():\n    helper()\n",
	}
	idx, ws := buildFixture(t, files)
	want := edgeKeys(t, idx)
	require.False(t, want["caller -calls-> example.com/x.helper"], "a clean build has no Python-to-Go edge")

	require.NoError(t, idx.store.SetMeta(metaBuildInProgress, []byte("killed-run")))
	require.NoError(t, idx.Update())
	assert.Equal(t, want, edgeKeys(t, idx), "recovery")

	require.NoError(t, os.WriteFile(filepath.Join(ws, "py", "a.py"), []byte("def caller():\n    helper()\n    pass\n"), 0o644))
	require.NoError(t, idx.Update())
	assert.Equal(t, want, edgeKeys(t, idx), "incremental update")
}

// wideCallFiles is n Python files whose caller calls every helper of the
// next 12 files, so a few files make more than one 1024-edge stride.
func wideCallFiles(n int) map[string]string {
	files := map[string]string{}
	for i := 0; i < n; i++ {
		var b strings.Builder
		fmt.Fprintf(&b, "def caller_%d():\n", i)
		for j := 1; j <= 12; j++ {
			for k := 0; k < 2; k++ {
				fmt.Fprintf(&b, "    helper_%d_%d()\n", (i+j)%n, k)
			}
		}
		for k := 0; k < 2; k++ {
			fmt.Fprintf(&b, "\ndef helper_%d_%d():\n    pass\n", i, k)
		}
		files[fmt.Sprintf("py/w%03d.py", i)] = b.String()
	}
	return files
}

// nonGoEdgeCount counts the edges that start at a non-Go symbol, read
// through its own Store, as another process's reader would.
func nonGoEdgeCount(t *testing.T, st *Store) int {
	t.Helper()
	var n int
	require.NoError(t, st.db.QueryRow(`SELECT COUNT(*) FROM edges
		WHERE source_id IN (SELECT id FROM symbols WHERE file NOT LIKE '%.go')`).Scan(&n))
	return n
}

// #393 review: recovery replaces the non-Go edges in one transaction, so a
// reader on another connection sees the old edges or the new ones, never
// a graph without them. No file changed here, so every edge must stay
// visible; the hook inside ReplaceNonGoEdges checks the delete is not
// committed on its own.
func TestUpdate_RecoveryReadersNeverSeeMissingNonGoEdges(t *testing.T) {
	files := wideCallFiles(60)
	idx, _ := buildFixture(t, files)
	want := edgeKeys(t, idx)
	reader, err := NewStore(idx.store.path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })
	before := nonGoEdgeCount(t, reader)
	require.Greater(t, before, 1024, "the fixture needs more than one resolve stride")
	require.NoError(t, idx.store.SetMeta(metaBuildInProgress, []byte("killed-run")))

	var seen []int
	testHookReresolveResolve = func() { seen = append(seen, nonGoEdgeCount(t, reader)) }
	afterDelete := -1
	testHookReplaceNonGoAfterDelete = func() { afterDelete = nonGoEdgeCount(t, reader) }
	t.Cleanup(func() { testHookReresolveResolve = nil; testHookReplaceNonGoAfterDelete = nil })
	require.NoError(t, idx.Update())
	require.Greater(t, len(seen), 1)
	assert.Equal(t, before, afterDelete, "between the delete and the inserts a reader still sees the old edges")
	for i, n := range seen {
		assert.Equal(t, before, n, "a reader during resolve stride %d sees every non-Go edge", i)
	}
	assert.Equal(t, before, nonGoEdgeCount(t, reader))
	assert.Equal(t, want, edgeKeys(t, idx))
}

// A recovery cancelled while it resolves the non-Go edges rolls back: every
// edge stays as it was and the mark stays set for the next run.
func TestUpdate_RecoveryCancelWhileResolvingKeepsEdges(t *testing.T) {
	files := wideCallFiles(60)
	idx, _ := buildFixture(t, files)
	want := edgeKeys(t, idx)
	require.NoError(t, idx.store.SetMeta(metaBuildInProgress, []byte("killed-run")))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	strides := 0
	testHookReresolveResolve = func() {
		strides++
		if strides == 2 {
			cancel()
		}
	}
	t.Cleanup(func() { testHookReresolveResolve = nil })
	require.ErrorIs(t, idx.UpdateWithContext(ctx), context.Canceled)
	assert.Equal(t, want, edgeKeys(t, idx), "a cancel while resolving changes no edge")
	mark, err := idx.store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	assert.NotNil(t, mark, "a cancelled recovery keeps the mark")

	testHookReresolveResolve = nil
	require.NoError(t, idx.Update())
	assert.Equal(t, want, edgeKeys(t, idx))
}

// A cancel while ReplaceNonGoEdges inserts rolls back the delete as well.
func TestStore_ReplaceNonGoEdgesCancelRollsBack(t *testing.T) {
	idx, _ := buildFixture(t, wideCallFiles(60))
	want := edgeKeys(t, idx)
	edges := make([]Edge, 0, 2048)
	rows, err := idx.store.db.Query(`SELECT source_id, target_id, kind FROM edges`)
	require.NoError(t, err)
	for rows.Next() {
		var e Edge
		require.NoError(t, rows.Scan(&e.SourceID, &e.TargetID, &e.Kind))
		edges = append(edges, e)
	}
	require.NoError(t, rows.Close())
	require.Greater(t, len(edges), 1024)

	// The first stride inserts, the second finds the context ended.
	err = idx.store.ReplaceNonGoEdges(&failAfterCtx{Context: context.Background(), n: 1}, edges[1:])
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, want, edgeKeys(t, idx), "a cancelled replacement changes no edge")

	require.NoError(t, idx.store.ReplaceNonGoEdges(context.Background(), edges[1:]))
	assert.Len(t, edgeKeys(t, idx), len(want)-1)
}
