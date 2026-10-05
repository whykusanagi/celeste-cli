package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

func indexTestWorkspace(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ws, "a.py"), []byte("def caller():\n    helper()\n\ndef helper():\n    pass\n"), 0o644))
	return ws
}

// `celeste index rebuild` builds a fresh index and exits 0; `reset`
// deletes it.
func TestIndexRebuildAndReset(t *testing.T) {
	ws := indexTestWorkspace(t)
	var out, errOut bytes.Buffer
	require.Equal(t, 0, runIndexRebuild(ws, &out, &errOut), errOut.String())
	assert.Contains(t, out.String(), "Completed in")
	_, err := os.Stat(codegraph.IndexPath(ws))
	require.NoError(t, err, "rebuild writes the index")

	out.Reset()
	errOut.Reset()
	require.Equal(t, 0, runIndexReset(ws, &out, &errOut), errOut.String())
	assert.Contains(t, out.String(), "Index deleted")
	_, err = os.Stat(codegraph.IndexPath(ws))
	assert.True(t, os.IsNotExist(err))
}
