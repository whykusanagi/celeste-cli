package codegraph

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// #389: PRAGMA foreign_keys=ON run through db.Exec only reached the one
// pooled connection that happened to run it, so ON DELETE CASCADE applied on
// some connections and not others. Every pooled connection must enforce
// foreign keys.
func TestStore_EveryConnectionEnforcesForeignKeys(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "codegraph.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		// Hold each connection so the next one is a fresh pool member.
		c, err := store.db.Conn(ctx)
		require.NoError(t, err)
		defer c.Close()
		var fk int
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk))
		require.Equal(t, 1, fk, "connection %d: foreign_keys should be on", i)
	}
}

// Deleting a symbol on any connection cascades to its BM25 tokens and LSH
// bands, which have no explicit cleanup in DeleteFileSymbols.
func TestStore_DeleteFileSymbolsCascadesOnEveryConnection(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "codegraph.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	// Pin the first pooled connection so the writes below run on others.
	pinned, err := store.db.Conn(ctx)
	require.NoError(t, err)
	defer pinned.Close()

	a, err := store.UpsertSymbol(Symbol{Name: "a", Kind: SymbolFunction, Package: "p", File: "x.py", Line: 1})
	require.NoError(t, err)
	b, err := store.UpsertSymbol(Symbol{Name: "b", Kind: SymbolFunction, Package: "p", File: "y.py", Line: 1})
	require.NoError(t, err)
	require.NoError(t, store.AddEdge(b, a, EdgeCalls))
	require.NoError(t, store.UpsertSymbolTokens(a, []string{"alpha", "beta"}))
	require.NoError(t, store.UpsertLSHBands(a, []uint64{1, 2, 3}))

	require.NoError(t, store.DeleteFileSymbols("x.py"))

	for _, c := range []struct {
		q    string
		args []any
	}{
		{`SELECT COUNT(*) FROM symbol_tokens WHERE symbol_id = ?`, []any{a}},
		{`SELECT COUNT(*) FROM lsh_bands WHERE symbol_id = ?`, []any{a}},
		{`SELECT COUNT(*) FROM edges WHERE source_id = ? OR target_id = ?`, []any{a, a}},
	} {
		var n int
		require.NoError(t, store.db.QueryRow(c.q, c.args...).Scan(&n))
		require.Zero(t, n, "rows left behind by %q", c.q)
	}
}
