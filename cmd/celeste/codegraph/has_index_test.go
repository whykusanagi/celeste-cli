package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #399: HasIndex tells a never-built index from a built one, including a
// built index of a workspace with no source files.
func TestHasIndex(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"with source", map[string]string{"main.go": "package main\n\nfunc main() {}\n"}},
		{"empty workspace", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
			}
			idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "cg.db"))
			require.NoError(t, err)
			defer idx.Close()

			has, err := idx.HasIndex()
			require.NoError(t, err)
			assert.False(t, has, "a freshly opened index has not been built")

			require.NoError(t, idx.Build())
			has, err = idx.HasIndex()
			require.NoError(t, err)
			assert.True(t, has, "a built index is an index")
		})
	}
}
