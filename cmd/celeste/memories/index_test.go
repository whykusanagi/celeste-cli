package memories

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadIndexEmpty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "MEMORY.md")

	idx, err := LoadIndex(path)
	require.NoError(t, err)
	assert.Empty(t, idx.Entries())
}

func TestIndexAddAndSave(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "MEMORY.md")

	idx, err := LoadIndex(path)
	require.NoError(t, err)

	idx.Add(IndexEntry{Name: "pref1", File: "pref1.md", Description: "User preference"})
	idx.Add(IndexEntry{Name: "decision1", File: "decision1.md", Description: "Architecture decision"})

	require.NoError(t, idx.Save())

	// Reload and verify.
	idx2, err := LoadIndex(path)
	require.NoError(t, err)
	assert.Len(t, idx2.Entries(), 2)
	assert.Equal(t, "pref1", idx2.Entries()[0].Name)
	assert.Equal(t, "decision1", idx2.Entries()[1].Name)
}

func TestIndexAddReplace(t *testing.T) {
	idx := &Index{path: "/tmp/test.md"}
	idx.Add(IndexEntry{Name: "x", File: "x.md", Description: "old"})
	idx.Add(IndexEntry{Name: "x", File: "x.md", Description: "new"})
	assert.Len(t, idx.Entries(), 1)
	assert.Equal(t, "new", idx.Entries()[0].Description)
}

func TestIndexRemove(t *testing.T) {
	idx := &Index{path: "/tmp/test.md"}
	idx.Add(IndexEntry{Name: "a", File: "a.md", Description: "A"})
	idx.Add(IndexEntry{Name: "b", File: "b.md", Description: "B"})

	require.NoError(t, idx.Remove("a"))
	assert.Len(t, idx.Entries(), 1)
	assert.Equal(t, "b", idx.Entries()[0].Name)
}

func TestIndexRemoveNotFound(t *testing.T) {
	idx := &Index{path: "/tmp/test.md"}
	err := idx.Remove("nope")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestIndexRender(t *testing.T) {
	idx := &Index{path: "/tmp/test.md"}
	idx.Add(IndexEntry{Name: "pref", File: "pref.md", Description: "A preference"})

	rendered := idx.Render()
	assert.Contains(t, rendered, "# Memories")
	assert.Contains(t, rendered, "**pref**")
	assert.Contains(t, rendered, "`pref.md`")
	assert.Contains(t, rendered, "A preference")
}

func TestIndexRenderEmpty(t *testing.T) {
	idx := &Index{path: "/tmp/test.md"}
	assert.Empty(t, idx.Render())
}

func TestIndexRenderTruncation(t *testing.T) {
	idx := &Index{path: "/tmp/test.md"}
	for i := 0; i < 250; i++ {
		idx.Add(IndexEntry{
			Name:        fmt.Sprintf("mem-%d", i),
			File:        fmt.Sprintf("mem-%d.md", i),
			Description: "A memory",
		})
	}
	rendered := idx.Render()
	assert.Contains(t, rendered, "truncated")
}
