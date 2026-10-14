package ctxmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReservedCompactionNamesCannotSpillOrBeRecalled(t *testing.T) {
	base := t.TempDir()
	store := filepath.Join(base, compactStoreDir)
	require.NoError(t, os.Mkdir(store, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(store, "call.txt"), []byte("compacted"), 0o600))
	for _, session := range []string{"pruned", "PRUNED", "PrUnEd"} {
		_, capped, err := CapToolResult(strings.Repeat(session, 1024), 128, session, "call", base)
		require.Error(t, err, "reserved session must fail rather than alias compaction data")
		require.False(t, capped)
		_, err = LoadSpilled(base, session+"/call")
		require.Error(t, err, "recall must not enter the compaction store")
	}
	body, err := os.ReadFile(filepath.Join(store, "call.txt"))
	require.NoError(t, err)
	require.Equal(t, "compacted", string(body))
	_, err = os.Stat(filepath.Join(base, "pruned-session"))
	require.ErrorIs(t, err, os.ErrNotExist, "reserved names must not remap to another valid session")
}

func TestPrunedSessionHasItsOwnDistinctSpill(t *testing.T) {
	base := t.TempDir()
	full := strings.Repeat("x", 4096)
	_, capped, err := CapToolResult(full, 1024, "pruned-session", "call", base)
	require.NoError(t, err)
	require.True(t, capped)
	got, err := LoadSpilled(base, "pruned-session/call")
	require.NoError(t, err)
	require.Equal(t, full, got)
	// An existing ordinary session must be unaffected by attempts to write the reserved name.
	_, _, err = CapToolResult("changed"+full, 1024, "pruned", "call", base)
	require.Error(t, err)
	got, err = LoadSpilled(base, "pruned-session/call")
	require.NoError(t, err)
	require.Equal(t, full, got)
}

func TestReservedCaseVariantNotCountedOrPruned(t *testing.T) {
	base := t.TempDir()
	for _, session := range []string{"PRUNED", "PrUnEd"} {
		dir := filepath.Join(base, session)
		if err := os.Mkdir(dir, 0o700); err != nil {
			// On case-insensitive volumes the spellings refer to the same directory.
			continue
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, spillMarkerName), []byte(spillMarkerBody), 0o600))
		file := filepath.Join(dir, "call.txt")
		require.NoError(t, os.WriteFile(file, []byte("data"), 0o600))
		past := time.Now().Add(-2 * SpillKeepAge)
		require.NoError(t, os.Chtimes(file, past, past))
		require.NoError(t, os.Chtimes(dir, past, past))
	}
	require.Zero(t, totalSpillBytes(base))
	require.NoError(t, PruneToolResults(base, "different", time.Now()))
	entries, err := os.ReadDir(base)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, entry := range entries {
		require.FileExists(t, filepath.Join(base, entry.Name(), "call.txt"))
	}
}
