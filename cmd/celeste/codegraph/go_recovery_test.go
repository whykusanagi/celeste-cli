package codegraph

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #394 (G1, G9 in the 2.0 final verification): a full build of a Go
// repository stamps no graph version until its last pass, so the update that
// recovers an interrupted first build or rebuild took the index for an old
// one and dropped every Go row first. Each short run started again from zero
// and never finished. These tests reproduce each case the verification
// recorded: a killed rebuild, repeated short runs, a fresh index over short
// sessions, and an upgrade from a v1.16 index.

// manyGoFiles is a Go module of n files in four packages, each function
// calling the next file's function in its package, plus a TypeScript and a
// Python pair whose calls run forward in file order, so a per-file
// (v1.16-style) index misses them and only a two-pass resolve finds them.
func manyGoFiles(n int) map[string]string {
	files := map[string]string{"go.mod": "module example.com/many\n\ngo 1.22\n"}
	for i := 0; i < n; i++ {
		pkg := fmt.Sprintf("p%d", i%4)
		next := (i + 4) % n
		files[fmt.Sprintf("%s/f%03d.go", pkg, i)] = fmt.Sprintf(
			"package %s\n\nfunc F%d() { F%d() }\n\nfunc G%d() int { return %d }\n", pkg, i, next, i, i)
	}
	return withNonGoPairs(files)
}

func withNonGoPairs(files map[string]string) map[string]string {
	files["py/a.py"] = "def caller():\n    helper()\n"
	files["py/b.py"] = "def helper():\n    pass\n"
	files["web/main.ts"] = "export function main() {\n  square(2);\n  push(3);\n}\n"
	files["web/math.ts"] = "export function square(n: number): number {\n  return n * n;\n}\n\n" +
		"export function push(n: number): void {\n  console.log(n);\n}\n"
	return files
}

// symbolKeys lists every stored symbol by name, kind, file and qualified
// name, sorted, so two indexes can be compared row for row.
func symbolKeys(t *testing.T, idx *Indexer) []string {
	t.Helper()
	rows, err := idx.store.db.Query(`SELECT name, kind, file, COALESCE(qual_name, '') FROM symbols`)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name, kind, file, qual string
		require.NoError(t, rows.Scan(&name, &kind, &file, &qual))
		out = append(out, name+"|"+kind+"|"+file+"|"+qual)
	}
	require.NoError(t, rows.Err())
	sort.Strings(out)
	return out
}

func goFileCount(t *testing.T, idx *Indexer) int {
	t.Helper()
	var n int
	require.NoError(t, idx.store.db.QueryRow(`SELECT COUNT(*) FROM files WHERE path LIKE '%.go'`).Scan(&n))
	return n
}

// killAfterGoFiles makes the Go pass of the next run stop after it has
// stored k files, as a process killed part-way through it would. It
// returns the run's context and, through first, the symbol count the
// store held when the run's Go pass stored its first file: a run that
// emptied the graph first shows it there.
func killAfterGoFiles(t *testing.T, idx *Indexer, k int, first *int) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stored := 0
	*first = -1
	testHookGoStore = func(string) {
		if stored == 0 {
			_, syms := graphCounts(t, idx)
			*first = syms
		}
		stored++
		if stored > k {
			cancel()
		}
	}
	t.Cleanup(func() { testHookGoStore = nil })
	return ctx
}

func requireFinished(t *testing.T, idx *Indexer) {
	t.Helper()
	for _, key := range []string{metaBuildInProgress, metaGoPassPending} {
		v, err := idx.store.GetMeta(key)
		require.NoError(t, err)
		assert.Nil(t, v, "%s must be cleared", key)
	}
	v, err := idx.store.GetMeta(metaGraphVersion)
	require.NoError(t, err)
	assert.Equal(t, graphVersion, string(v))
}

// Evidence: `celeste index rebuild` killed part-way, then `celeste index`
// dropped from 7,363 symbols to 16 and rebuilt everything. The recovery run
// must keep every row the killed rebuild stored and finish the graph.
func TestRebuild_KilledInGoPassIsResumedNotEmptied(t *testing.T) {
	requireGoToolchain(t)
	files := manyGoFiles(40)
	ref, _ := buildFixture(t, files)
	want, wantSyms := edgeKeys(t, ref), symbolKeys(t, ref)
	requireEdges(t, want, "caller -calls-> helper", "main -calls-> square", "main -calls-> push",
		"example.com/many/p0.F0 -calls-> example.com/many/p0.F4")

	ws := writeFixture(t, files)
	db := filepath.Join(t.TempDir(), "cg.db")
	idx, err := NewIndexer(ws, db)
	require.NoError(t, err)
	require.NoError(t, idx.Build())
	require.NoError(t, idx.Close())

	// The rebuild opens its own Indexer; the hook stops its Go pass after
	// 15 stored files.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stored := 0
	testHookGoStore = func(string) {
		stored++
		if stored > 15 {
			cancel()
		}
	}
	t.Cleanup(func() { testHookGoStore = nil })
	_, _, err = Rebuild(ctx, ws, db)
	require.ErrorIs(t, err, context.Canceled)

	idx = openIndexer(t, ws, db)
	killedFiles, killedSyms := graphCounts(t, idx)
	require.Equal(t, 15, goFileCount(t, idx), "the killed rebuild stored 15 Go files")
	mark, err := idx.store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	require.NotNil(t, mark)

	var first int
	_ = killAfterGoFiles(t, idx, 1<<30, &first)
	require.NoError(t, idx.Update())
	assert.GreaterOrEqual(t, first, killedSyms, "the recovery run must not drop the rows the killed rebuild stored")
	nFiles, _ := graphCounts(t, idx)
	assert.GreaterOrEqual(t, nFiles, killedFiles)
	assert.Equal(t, want, edgeKeys(t, idx))
	assert.Equal(t, wantSyms, symbolKeys(t, idx))
	requireFinished(t, idx)
}

// shortRuns drives runs that each stop after storing k Go files until one
// finishes, checking that no run drops what the earlier ones stored, and
// that each one stores more. run performs one run with the given context
// and returns its error.
func shortRuns(t *testing.T, k, total int, idxOf func() *Indexer, run func(*Indexer, context.Context) error) *Indexer {
	t.Helper()
	prevGo, prevSyms := -1, -1
	for i := 0; i < 2*total/k+4; i++ {
		idx := idxOf()
		var first int
		ctx := killAfterGoFiles(t, idx, k, &first)
		err := run(idx, ctx)
		if first >= 0 && prevSyms >= 0 {
			require.GreaterOrEqual(t, first, prevSyms, "run %d emptied the graph before its Go pass", i)
		}
		_, syms := graphCounts(t, idx)
		nGo := goFileCount(t, idx)
		if prevSyms >= 0 {
			require.GreaterOrEqual(t, syms, prevSyms, "run %d dropped symbols", i)
		}
		if err == nil {
			require.Equal(t, total, nGo)
			return idx
		}
		require.ErrorIs(t, err, context.Canceled)
		require.Greater(t, nGo, prevGo, "run %d stored no new Go file", i)
		prevGo, prevSyms = nGo, syms
	}
	t.Fatalf("short runs never finished the index")
	return nil
}

// Evidence: four `celeste index` runs on a fresh index, each killed at 12 s,
// each started again at 16 symbols and never passed ~5,000 of 15,422.
func TestUpdate_RepeatedShortRunsOnGoRepoReachFullCount(t *testing.T) {
	requireGoToolchain(t)
	files := manyGoFiles(40)
	ref, _ := buildFixture(t, files)
	want, wantSyms := edgeKeys(t, ref), symbolKeys(t, ref)

	ws := writeFixture(t, files)
	idx := openIndexer(t, ws, filepath.Join(t.TempDir(), "cg.db"))
	runs := 0
	got := shortRuns(t, 7, 40, func() *Indexer { return idx }, func(idx *Indexer, ctx context.Context) error {
		runs++
		if runs == 1 {
			return idx.BuildWithContext(ctx)
		}
		return idx.UpdateWithContext(ctx)
	})
	assert.Equal(t, want, edgeKeys(t, got))
	assert.Equal(t, wantSyms, symbolKeys(t, got))
	requireFinished(t, got)
}

// Evidence: three TUI sessions of about 14 s each on a fresh HOME went
// 552 -> 16 -> 3,249 -> 16 -> 2,773. Each session opens the index, updates
// it until it closes, and the next session opens it again.
func TestUpdate_FreshIndexAcrossShortSessionsReachesFullCount(t *testing.T) {
	requireGoToolchain(t)
	files := manyGoFiles(40)
	ref, _ := buildFixture(t, files)
	want, wantSyms := edgeKeys(t, ref), symbolKeys(t, ref)

	ws := writeFixture(t, files)
	db := filepath.Join(t.TempDir(), "cg.db")
	var open *Indexer
	got := shortRuns(t, 9, 40, func() *Indexer {
		if open != nil {
			require.NoError(t, open.Close())
		}
		var err error
		open, err = NewIndexer(ws, db)
		require.NoError(t, err)
		return open
	}, func(idx *Indexer, ctx context.Context) error { return idx.UpdateWithContext(ctx) })
	t.Cleanup(func() { _ = got.Close() })
	assert.Equal(t, want, edgeKeys(t, got))
	assert.Equal(t, wantSyms, symbolKeys(t, got))
	requireFinished(t, got)
}

// v1Index writes the index v1.16 left: every file indexed on its own in
// walk order (its Update path), so a call to a file later in the order has
// no edge; Go edges resolved by name; no qualified names and no graph
// version. extra adds a row v1.16's parsers gave that 2.0's do not.
func v1Index(t *testing.T, ws, db string) {
	t.Helper()
	idx, err := NewIndexer(ws, db)
	require.NoError(t, err)
	defer func() { require.NoError(t, idx.Close()) }()
	files, err := idx.walkSourceFiles()
	require.NoError(t, err)
	for _, f := range files {
		require.NoError(t, idx.indexFile(f))
	}
	_, err = idx.store.UpsertSymbol(Symbol{Name: "v1only", Kind: SymbolFunction, File: "py/a.py", Line: 1})
	require.NoError(t, err)
	_, err = idx.store.db.Exec(`UPDATE symbols SET qual_name = NULL, implements = NULL`)
	require.NoError(t, err)
	require.NoError(t, idx.store.DeleteMeta(metaGraphVersion))
	got := edgeKeys(t, idx)
	forbidEdges(t, got, "caller -calls-> helper", "main -calls-> square", "main -calls-> push")
}

// Evidence (G9): on a mixed Go+TS+Python repo the upgraded index kept
// v1.16's non-Go edges, 17 where a fresh 2.0 build has 19 (TS main ->
// square and main -> push missing). An upgrade must leave exactly the
// graph a fresh build does.
func TestUpgrade_V1IndexMatchesFreshBuild(t *testing.T) {
	requireGoToolchain(t)
	files := manyGoFiles(12)
	ref, _ := buildFixture(t, files)
	want, wantSyms := edgeKeys(t, ref), symbolKeys(t, ref)

	ws := writeFixture(t, files)
	db := filepath.Join(t.TempDir(), "cg.db")
	v1Index(t, ws, db)

	idx := openIndexer(t, ws, db)
	require.NoError(t, idx.Update())
	assert.Equal(t, want, edgeKeys(t, idx))
	assert.Equal(t, len(want), len(edgeKeys(t, idx)))
	assert.Equal(t, wantSyms, symbolKeys(t, idx))
	requireFinished(t, idx)
}

// An upgrade cut short again and again keeps the other languages' rows,
// keeps what each run stored, and ends at the fresh build's graph.
func TestUpgrade_V1IndexAcrossShortRunsMatchesFreshBuild(t *testing.T) {
	requireGoToolchain(t)
	files := manyGoFiles(40)
	ref, _ := buildFixture(t, files)
	want, wantSyms := edgeKeys(t, ref), symbolKeys(t, ref)

	ws := writeFixture(t, files)
	db := filepath.Join(t.TempDir(), "cg.db")
	v1Index(t, ws, db)

	idx := openIndexer(t, ws, db)
	var first int
	ctx := killAfterGoFiles(t, idx, 8, &first)
	err := idx.UpdateWithContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
	syms, err := idx.store.SearchSymbolsByName("helper")
	require.NoError(t, err)
	assert.NotEmpty(t, syms, "a cut-short upgrade keeps other languages' symbols")
	require.Equal(t, 8, goFileCount(t, idx))

	got := shortRuns(t, 8, 40, func() *Indexer { return idx }, func(idx *Indexer, ctx context.Context) error {
		return idx.UpdateWithContext(ctx)
	})
	assert.Equal(t, want, edgeKeys(t, got))
	assert.Equal(t, wantSyms, symbolKeys(t, got))
	requireFinished(t, got)
}
