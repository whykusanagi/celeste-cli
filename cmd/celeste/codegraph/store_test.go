package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_CreateAndClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "codegraph.db")

	store, err := NewStore(dbPath)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	// Verify the database file exists
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}

func TestStore_UpsertAndGetSymbol(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	sym := Symbol{
		Name:      "HandleRequest",
		Kind:      SymbolFunction,
		Package:   "server",
		File:      "cmd/server/handler.go",
		Line:      42,
		Signature: "func HandleRequest(w http.ResponseWriter, r *http.Request)",
	}

	id, err := store.UpsertSymbol(sym)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Retrieve by ID
	got, err := store.GetSymbol(id)
	require.NoError(t, err)
	assert.Equal(t, sym.Name, got.Name)
	assert.Equal(t, sym.Kind, got.Kind)
	assert.Equal(t, sym.Package, got.Package)
	assert.Equal(t, sym.File, got.File)
	assert.Equal(t, sym.Line, got.Line)
	assert.Equal(t, sym.Signature, got.Signature)
}

func TestStore_UpsertSymbol_Upsert(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	sym := Symbol{
		Name:    "Foo",
		Kind:    SymbolFunction,
		Package: "main",
		File:    "main.go",
		Line:    10,
	}

	id1, err := store.UpsertSymbol(sym)
	require.NoError(t, err)

	// Upsert with updated line
	sym.Line = 20
	id2, err := store.UpsertSymbol(sym)
	require.NoError(t, err)
	assert.Equal(t, id1, id2, "upsert should return same ID")

	got, err := store.GetSymbol(id1)
	require.NoError(t, err)
	assert.Equal(t, 20, got.Line)
}

func TestStore_AddAndQueryEdges(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Create two symbols
	caller := Symbol{Name: "main", Kind: SymbolFunction, Package: "main", File: "main.go", Line: 1}
	callee := Symbol{Name: "serve", Kind: SymbolFunction, Package: "server", File: "server.go", Line: 5}

	callerID, err := store.UpsertSymbol(caller)
	require.NoError(t, err)
	calleeID, err := store.UpsertSymbol(callee)
	require.NoError(t, err)

	// Add edge
	err = store.AddEdge(callerID, calleeID, EdgeCalls)
	require.NoError(t, err)

	// Query outgoing edges
	edges, err := store.GetEdgesFrom(callerID)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, calleeID, edges[0].TargetID)
	assert.Equal(t, EdgeCalls, edges[0].Kind)

	// Query incoming edges
	edges, err = store.GetEdgesTo(calleeID)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, callerID, edges[0].SourceID)
}

func TestStore_UpsertAndGetFile(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	f := FileRecord{
		Path:        "cmd/server/handler.go",
		Language:    "go",
		Size:        4096,
		ContentHash: "abc123",
	}

	err := store.UpsertFile(f)
	require.NoError(t, err)

	got, err := store.GetFile(f.Path)
	require.NoError(t, err)
	assert.Equal(t, f.Language, got.Language)
	assert.Equal(t, f.Size, got.Size)
	assert.Equal(t, f.ContentHash, got.ContentHash)
	assert.Greater(t, got.IndexedAt, int64(0))
}

func TestStore_DeleteFileSymbols(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Create symbols for a file
	sym1 := Symbol{Name: "A", Kind: SymbolFunction, Package: "pkg", File: "foo.go", Line: 1}
	sym2 := Symbol{Name: "B", Kind: SymbolFunction, Package: "pkg", File: "foo.go", Line: 10}
	sym3 := Symbol{Name: "C", Kind: SymbolFunction, Package: "pkg", File: "bar.go", Line: 1}

	_, err := store.UpsertSymbol(sym1)
	require.NoError(t, err)
	_, err = store.UpsertSymbol(sym2)
	require.NoError(t, err)
	id3, err := store.UpsertSymbol(sym3)
	require.NoError(t, err)

	// Delete symbols for foo.go
	err = store.DeleteFileSymbols("foo.go")
	require.NoError(t, err)

	// bar.go symbols should survive
	got, err := store.GetSymbol(id3)
	require.NoError(t, err)
	assert.Equal(t, "C", got.Name)

	// foo.go symbols should be gone
	syms, err := store.GetSymbolsByFile("foo.go")
	require.NoError(t, err)
	assert.Empty(t, syms)
}

func TestStore_GetSymbolsByFile(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		_, err := store.UpsertSymbol(Symbol{
			Name: name, Kind: SymbolFunction, Package: "pkg", File: "target.go", Line: 1,
		})
		require.NoError(t, err)
	}
	// Different file
	_, err := store.UpsertSymbol(Symbol{
		Name: "Delta", Kind: SymbolFunction, Package: "pkg", File: "other.go", Line: 1,
	})
	require.NoError(t, err)

	syms, err := store.GetSymbolsByFile("target.go")
	require.NoError(t, err)
	assert.Len(t, syms, 3)
}

func TestStore_GetSymbolsByPackage(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	for _, name := range []string{"X", "Y"} {
		_, err := store.UpsertSymbol(Symbol{
			Name: name, Kind: SymbolFunction, Package: "auth", File: "auth.go", Line: 1,
		})
		require.NoError(t, err)
	}
	_, err := store.UpsertSymbol(Symbol{
		Name: "Z", Kind: SymbolFunction, Package: "server", File: "server.go", Line: 1,
	})
	require.NoError(t, err)

	syms, err := store.GetSymbolsByPackage("auth")
	require.NoError(t, err)
	assert.Len(t, syms, 2)
}

func TestStore_SearchSymbolsByName(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	for _, name := range []string{"HandleRequest", "HandleResponse", "ServeHTTP"} {
		_, err := store.UpsertSymbol(Symbol{
			Name: name, Kind: SymbolFunction, Package: "server", File: "server.go", Line: 1,
		})
		require.NoError(t, err)
	}

	results, err := store.SearchSymbolsByName("Handle")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestStore_UpdateMinHash(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	id, err := store.UpsertSymbol(Symbol{
		Name: "Foo", Kind: SymbolFunction, Package: "pkg", File: "foo.go", Line: 1,
	})
	require.NoError(t, err)

	sig := MinHashSignature{1, 2, 3, 4, 5}
	err = store.UpdateMinHash(id, sig)
	require.NoError(t, err)

	got, err := store.GetMinHash(id)
	require.NoError(t, err)
	assert.Equal(t, sig, got)
}

func TestStore_GetAllMinHashes(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	id1, _ := store.UpsertSymbol(Symbol{Name: "A", Kind: SymbolFunction, Package: "p", File: "a.go", Line: 1})
	id2, _ := store.UpsertSymbol(Symbol{Name: "B", Kind: SymbolFunction, Package: "p", File: "b.go", Line: 1})
	_ = store.UpdateMinHash(id1, MinHashSignature{10, 20, 30})
	_ = store.UpdateMinHash(id2, MinHashSignature{40, 50, 60})

	all, err := store.GetAllMinHashes()
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestStore_Stats(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	_, _ = store.UpsertSymbol(Symbol{Name: "A", Kind: SymbolFunction, Package: "p", File: "a.go", Line: 1})
	_, _ = store.UpsertSymbol(Symbol{Name: "B", Kind: SymbolType, Package: "p", File: "a.go", Line: 10})
	_ = store.UpsertFile(FileRecord{Path: "a.go", Language: "go", Size: 100, ContentHash: "x"})

	stats, err := store.Stats()
	require.NoError(t, err)
	assert.Equal(t, 2, stats.TotalSymbols)
	assert.Equal(t, 1, stats.TotalFiles)
}

// newTestStore creates a temporary in-memory store for testing.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	return store
}
