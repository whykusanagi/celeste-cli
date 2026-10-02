package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
)

// The .grimoire auto-stamp (cleanup-5 D4) writes through writeFileFunc on
// the checked path, as the edit itself does, not os.WriteFile on the path
// the model gave; its header is grimoire.GrimoireMeta plus the index line.
func TestGrimoireStampWritesTheCheckedPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	real := t.TempDir()
	ws := filepath.Join(t.TempDir(), "ws")
	if err := os.Symlink(real, ws); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	idx := codegraph.IndexPath(ws)
	put(t, idx, "db")

	var paths []string
	orig := writeFileFunc
	writeFileFunc = func(name string, data []byte, perm os.FileMode) error {
		paths = append(paths, name)
		return orig(name, data, perm)
	}
	t.Cleanup(func() { writeFileFunc = orig })

	res := run(t, NewWriteFileTool(ws), context.Background(), map[string]any{"path": ".grimoire", "content": "## Bindings\n- tabs\n"})
	require.False(t, res.Error, res.Content)

	require.Len(t, paths, 2, "the edit and the stamp both go through writeFileFunc")
	assert.Equal(t, paths[0], paths[1], "the stamp writes the path the edit wrote")
	got := get(t, filepath.Join(real, ".grimoire"))
	assert.True(t, strings.HasPrefix(got, "<!--\nlast_updated: "), got)
	assert.Contains(t, got, "index: indexed ")
	assert.True(t, strings.HasSuffix(got, "-->\n\n## Bindings\n- tabs\n"), got)

	// A second write replaces the header instead of adding one.
	res = run(t, NewWriteFileTool(ws), context.Background(), map[string]any{"path": ".grimoire", "content": get(t, filepath.Join(real, ".grimoire"))})
	require.False(t, res.Error, res.Content)
	assert.Equal(t, 1, strings.Count(get(t, filepath.Join(real, ".grimoire")), "<!--"))
}
