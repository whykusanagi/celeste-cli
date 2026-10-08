package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// TestLoadConfigRejectsOversizedFile: a config over the size cap fails
// instead of being read whole into memory (Aikido 806869859).
func TestLoadConfigRejectsOversizedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mcp.json")
	big := `{"mcpServers":{}}` + strings.Repeat(" ", maxConfigBytes)
	require.NoError(t, os.WriteFile(p, []byte(big), 0o600))
	_, err := LoadConfig(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

// TestDiscoverConfigPathsSkipsNonRegularWorkspaceFiles: a workspace
// .mcp.json that is not a regular file (here a directory link) is not
// a config candidate (Aikido 806869859).
func TestDiscoverConfigPathsSkipsNonRegularWorkspaceFiles(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".mcp.json"), []byte(`{"mcpServers":{}}`), 0o600))
	assert.Equal(t, []string{filepath.Join(ws, ".mcp.json")}, DiscoverConfigPaths(ws, home))

	ws2 := t.TempDir()
	target := filepath.Join(t.TempDir(), "dir")
	require.NoError(t, os.Mkdir(target, 0o755))
	if err := os.Symlink(target, filepath.Join(ws2, ".mcp.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	assert.Empty(t, DiscoverConfigPaths(ws2, home))
}

// TestLoadMergedLenientKeepsGlobalOnWorkspaceParseError: a malformed
// workspace config is skipped with a warning; the home configs still load
// (Aikido 806869709).
func TestLoadMergedLenientKeepsGlobalOnWorkspaceParseError(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	global := filepath.Join(home, ".celeste", "mcp.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(global), 0o700))
	require.NoError(t, os.WriteFile(global, []byte(`{"mcpServers":{"mine":{"command":"true","enabled":true}}}`), 0o600))
	bad := filepath.Join(ws, ".mcp.json")
	require.NoError(t, os.WriteFile(bad, []byte(`{not json`), 0o600))

	paths := DiscoverConfigPaths(ws, home)
	cfg, skipped, err := LoadMergedLenient(paths, home)
	require.NoError(t, err)
	require.Len(t, skipped, 1)
	assert.Contains(t, skipped[0].Error(), bad)
	assert.Contains(t, cfg.Servers, "mine")

	// The manager keeps the home server too.
	m := NewManagerMulti(paths, tools.NewRegistry())
	m.home = home
	mcfg, err := m.loadConfig()
	require.NoError(t, err)
	assert.Contains(t, mcfg.Servers, "mine")

	// A malformed home config still fails the load.
	require.NoError(t, os.WriteFile(global, []byte(`{nope`), 0o600))
	_, _, err = LoadMergedLenient(DiscoverConfigPaths(ws, home), home)
	require.Error(t, err)
}

// TestSkippedWorkspaceConfigsNamePathAndAreTyped: a workspace config skipped
// for any reason (not a regular file, too large, malformed) names its path,
// and Start's error is recognisable as a skip, not a failure.
func TestSkippedWorkspaceConfigsNamePathAndAreTyped(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	dirCfg := filepath.Join(ws, "dir.json")
	require.NoError(t, os.Mkdir(dirCfg, 0o755))
	bigCfg := filepath.Join(ws, "big.json")
	require.NoError(t, os.WriteFile(bigCfg, []byte(`{"mcpServers":{}}`+strings.Repeat(" ", maxConfigBytes)), 0o600))

	_, skipped, err := LoadMergedLenient([]string{dirCfg, bigCfg}, home)
	require.NoError(t, err)
	require.Len(t, skipped, 2)
	assert.Contains(t, skipped[0].Error(), dirCfg)
	assert.Contains(t, skipped[1].Error(), bigCfg)

	m := NewManagerMulti([]string{dirCfg}, tools.NewRegistry())
	m.home = home
	err = m.Start(t.Context())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkspaceConfigSkipped), "Start's skip error is not typed: %v", err)
	sk := SkippedConfigs(err)
	require.Len(t, sk, 1)
	assert.Equal(t, dirCfg, sk[0].Path)
}
