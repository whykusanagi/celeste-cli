package codegraph

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Name resolution never crosses languages (G8 of #395): a TypeScript call
// to add does not land on a Python add stored before it. TypeScript and
// JavaScript resolve to each other, and so do C and C++ (headers are .h).
func TestStore_NameResolutionStaysInLanguage(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()
	up := func(name string, kind SymbolKind, file string) int64 {
		id, err := s.UpsertSymbol(Symbol{Name: name, Kind: kind, File: file, Line: 1})
		require.NoError(t, err)
		return id
	}
	up("add", SymbolFunction, "a/core.py")
	tsAdd := up("add", SymbolFunction, "src/math.ts")
	jsHelper := up("helper", SymbolFunction, "lib/util.js")
	hArea := up("area", SymbolFunction, "inc/geo.h")
	up("only_py", SymbolFunction, "a/core.py")
	up("Celsius", SymbolType, "temp.go")

	id, ok := s.GetCallableIDByName("add", "src/main.ts")
	require.True(t, ok)
	assert.Equal(t, tsAdd, id, "a TS call resolves to the TS add, not the Python one stored first")
	id, ok = s.GetSymbolIDByNameInFile("add", "src/main.tsx")
	require.True(t, ok)
	assert.Equal(t, tsAdd, id)

	_, ok = s.GetCallableIDByName("add", "app/x.rb")
	assert.False(t, ok, "Ruby has no add")
	_, ok = s.GetCallableIDByName("only_py", "src/main.ts")
	assert.False(t, ok, "a Python-only name does not resolve from TS")
	_, ok = s.GetSymbolIDByNameInFile("only_py", "x.go")
	assert.False(t, ok, "nor from Go")
	_, ok = s.GetCallableIDByName("Celsius", "src/main.ts")
	assert.False(t, ok, "a Go symbol never resolves from another language")

	id, ok = s.GetCallableIDByName("helper", "src/main.ts")
	require.True(t, ok)
	assert.Equal(t, jsHelper, id, "TS resolves to JS")
	id, ok = s.GetCallableIDByName("area", "src/shapes.cpp")
	require.True(t, ok)
	assert.Equal(t, hArea, id, "C++ resolves to a .h declaration")
}

// A build resolves a Ruby call to the Ruby function, not to the Python one
// with the same name that walk order stores first.
func TestIndexer_CallsDoNotCrossLanguages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a_core.py", "def add(a, b):\n    return a + b\n")
	writeFile(t, dir, "b_run.rb", "def run_ruby\n  add(1, 2)\nend\n")
	writeFile(t, dir, "c_math.rb", "def add(a, b)\n  a + b\nend\n")

	idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "codegraph.db"))
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())

	run := symbolIn(t, idx.Store(), "run_ruby", "b_run.rb")
	edges, err := idx.Store().GetEdgesFrom(run)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	target, err := idx.Store().GetSymbol(edges[0].TargetID)
	require.NoError(t, err)
	assert.Equal(t, "c_math.rb", target.File)
}

// An index built before G8 holds cross-language edges. The next Update
// resolves every non-Go edge again, once, and records that it did.
func TestIndexer_UpdateDropsCrossLanguageEdgesOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a_core.py", "def add(a, b):\n    return a + b\n")
	writeFile(t, dir, "b_run.rb", "def run_ruby\n  add(1, 2)\nend\n")
	writeFile(t, dir, "c_math.rb", "def add(a, b)\n  a + b\nend\n")

	idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "codegraph.db"))
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())
	scope, err := idx.Store().GetMeta(metaEdgeScope)
	require.NoError(t, err)
	assert.Equal(t, edgeScope, string(scope), "a build records the edge scope")

	// Make it look like an older index: the Ruby call went to Python.
	store := idx.Store()
	run := symbolIn(t, store, "run_ruby", "b_run.rb")
	pyAdd := symbolIn(t, store, "add", "a_core.py")
	rbAdd := symbolIn(t, store, "add", "c_math.rb")
	_, err = store.db.Exec(`DELETE FROM edges WHERE source_id = ?`, run)
	require.NoError(t, err)
	require.NoError(t, store.AddEdge(run, pyAdd, EdgeCalls))
	require.NoError(t, store.DeleteMeta(metaEdgeScope))

	require.NoError(t, idx.Update())
	edges, err := store.GetEdgesFrom(run)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, rbAdd, edges[0].TargetID)
	scope, err = store.GetMeta(metaEdgeScope)
	require.NoError(t, err)
	assert.Equal(t, edgeScope, string(scope))
}

func symbolIn(t *testing.T, s *Store, name, file string) int64 {
	t.Helper()
	syms, err := s.GetSymbolsByFile(file)
	require.NoError(t, err)
	for _, sym := range syms {
		if sym.Name == name {
			return sym.ID
		}
	}
	t.Fatalf("no symbol %s in %s", name, file)
	return 0
}

// The Go side of the one-time refresh: a Go file that did not type-check
// keeps name-resolved edges, and an older index may have sent one of them
// to a Python function. The next Update drops it.
func TestIndexer_UpdateDropsCrossLanguageGoEdgesOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/m\n\ngo 1.21\n")
	writeFile(t, dir, "a_core.py", "def add(a, b):\n    return a + b\n")
	writeFile(t, dir, "broken.go", "package m\n\nfunc Run() {\n\tundefinedThing()\n\thelperX()\n}\n\nfunc helperX() {}\n")

	idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "codegraph.db"))
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())

	store := idx.Store()
	run := symbolIn(t, store, "Run", "broken.go")
	pyAdd := symbolIn(t, store, "add", "a_core.py")
	require.NoError(t, store.AddEdge(run, pyAdd, EdgeCalls))
	require.NoError(t, store.DeleteMeta(metaEdgeScope))

	require.NoError(t, idx.Update())
	edges, err := store.GetEdgesFrom(run)
	require.NoError(t, err)
	for _, e := range edges {
		assert.NotEqual(t, pyAdd, e.TargetID, "the Go-to-Python edge is gone")
	}
	scope, err := store.GetMeta(metaEdgeScope)
	require.NoError(t, err)
	assert.Equal(t, edgeScope, string(scope))
}

// A file of no known language never resolves to a Go symbol, as the old
// non-Go filter guaranteed.
func TestStore_UnknownLanguageNeverResolvesToGo(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()
	_, err := s.UpsertSymbol(Symbol{Name: "Celsius", Kind: SymbolType, File: "temp.go", Line: 1})
	require.NoError(t, err)
	_, ok := s.GetCallableIDByName("Celsius", "notes.unknownext")
	assert.False(t, ok)
	_, ok = s.GetSymbolIDByNameInFile("Celsius", "notes.unknownext")
	assert.False(t, ok)
}
