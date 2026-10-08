package checkpoints

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An entry's root, or a directory above it, replaced by a symlink between
// the root check and opening it does not send the restore outside: the
// root is opened one component at a time without following a symlink
// (Aikido review of #421).
func TestUndoRefusesRootSwappedBeforeOpen(t *testing.T) {
	for _, swapParent := range []bool{false, true} {
		base, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		parent := filepath.Join(base, "p")
		ws := filepath.Join(parent, "ws")
		require.NoError(t, os.MkdirAll(ws, 0o755))
		write(t, filepath.Join(ws, "f.txt"), "after")
		evil := filepath.Join(base, "evil")
		require.NoError(t, os.MkdirAll(filepath.Join(evil, "ws"), 0o755))
		write(t, filepath.Join(evil, "ws", "f.txt"), "OUTSIDE")
		swapped, target := ws, filepath.Join(evil, "ws")
		if swapParent {
			swapped, target = parent, evil
		}
		testHookBeforeRootOpen = func() {
			require.NoError(t, os.Rename(swapped, swapped+".moved"))
			if err := os.Symlink(target, swapped); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}
		ref, err := openRef(Entry{Path: filepath.Join(ws, "f.txt"), Root: ws, Rel: "f.txt"})
		testHookBeforeRootOpen = nil
		if err == nil {
			_ = ref.replace([]byte("restored"), 0o644)
			ref.close()
		}
		assert.Error(t, err, "swapParent=%v", swapParent)
		assert.Equal(t, "OUTSIDE", read(t, filepath.Join(evil, "ws", "f.txt")), "swapParent=%v", swapParent)
	}
}
