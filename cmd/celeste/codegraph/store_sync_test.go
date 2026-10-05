package codegraph

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// #385: every autocommitted write used to fsync the WAL (synchronous=FULL),
// which costs tens of milliseconds a time on Windows; an index build issues
// about a hundred per symbol. Every pooled connection, not just the first,
// must run with synchronous=NORMAL.
func TestStore_EveryConnectionSyncsNormal(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "codegraph.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		// Hold each connection so the next one is a fresh pool member.
		c, err := store.db.Conn(ctx)
		require.NoError(t, err)
		defer c.Close()
		var mode, journal string
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&mode))
		require.Equal(t, "1", mode, "connection %d: synchronous should be NORMAL (1)", i)
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal))
		require.Equal(t, "wal", journal, "connection %d", i)
	}
}

// Durability across a reopen is unaffected: NORMAL in WAL mode only defers
// the fsync to checkpoints, and a clean Close checkpoints.
func TestStore_WritesSurviveReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codegraph.db")
	store, err := NewStore(dbPath)
	require.NoError(t, err)
	id, err := store.UpsertSymbol(Symbol{Name: "f", Kind: SymbolFunction, Package: "p", File: "a.go", Line: 1})
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = NewStore(dbPath)
	require.NoError(t, err)
	defer store.Close()
	sym, err := store.GetSymbol(id)
	require.NoError(t, err)
	require.Equal(t, "f", sym.Name)
}
