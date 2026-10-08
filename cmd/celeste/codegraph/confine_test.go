package codegraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const outsideSource = "package a\n\nfunc Leak() {\n\t// TODO: OUTSIDE-SECRET\n\tpanic(\"OUTSIDE-SECRET\")\n}\n"

func smellText(t *testing.T, idx *Indexer) string {
	t.Helper()
	smells, err := idx.FindCodeSmells(nil, 1000, true)
	require.NoError(t, err)
	var b strings.Builder
	for _, s := range smells {
		b.WriteString(s.File + " " + s.Name + " " + s.Snippet + " " + s.Reason + "\n")
	}
	return b.String()
}

// Aikido 806869369: a workspace file that is a symlink is neither indexed
// nor read by code review, so review cannot quote source from outside the
// workspace.
func TestSymlinkedSourceStaysOutOfIndexAndReview(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.go")
	require.NoError(t, os.WriteFile(outside, []byte(outsideSource), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "a.go"), []byte("package a\n\nfunc Real() {}\n"), 0o644))
	if err := os.Symlink(outside, filepath.Join(ws, "b.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.Build())
	syms, err := idx.store.GetSymbolsByFile("b.go")
	require.NoError(t, err)
	require.Empty(t, syms, "a symlinked file was indexed")
	require.NotContains(t, smellText(t, idx), "OUTSIDE-SECRET")
}

// A file indexed while regular and replaced by a symlink afterwards is not
// read through the symlink by review.
func TestReviewDoesNotReadAFileSwappedForASymlink(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.go")
	require.NoError(t, os.WriteFile(outside, []byte(outsideSource), 0o644))
	b := filepath.Join(ws, "b.go")
	require.NoError(t, os.WriteFile(b, []byte("package a\n\nfunc Leak() {\n\t// TODO: inside\n\tpanic(\"inside\")\n}\n"), 0o644))
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.Build())
	require.Contains(t, smellText(t, idx), "TODO: inside", "the fixture reports its marker")
	require.NoError(t, os.Remove(b))
	if err := os.Symlink(outside, b); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	require.NotContains(t, smellText(t, idx), "OUTSIDE-SECRET")
}
