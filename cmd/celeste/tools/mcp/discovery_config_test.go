package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestServerConfig_HasOriginField(t *testing.T) {
	// Origin is populated by the merge layer, never from JSON.
	sc := ServerConfig{Origin: "/home/u/.celeste/mcp.json"}
	assert.Equal(t, "/home/u/.celeste/mcp.json", sc.Origin)
}

func TestDiscoverConfigPaths_OrderAndExistence(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	// Create two of the five candidate files.
	celesteHome := filepath.Join(home, ".celeste")
	require.NoError(t, os.MkdirAll(celesteHome, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(celesteHome, "mcp.json"), []byte(`{"mcpServers":{}}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, ".mcp.json"), []byte(`{"mcpServers":{}}`), 0644))

	paths := DiscoverConfigPaths(cwd, home)

	require.Equal(t, []string{
		filepath.Join(home, ".celeste", "mcp.json"),
		filepath.Join(cwd, ".mcp.json"),
	}, paths)
}

func TestDiscoverConfigPaths_EmptyWhenNothingExists(t *testing.T) {
	assert.Empty(t, DiscoverConfigPaths(t.TempDir(), t.TempDir()))
}

func TestLoadMerged_LaterOverridesEarlier(t *testing.T) {
	dir := t.TempDir()
	low := filepath.Join(dir, "low.json")
	high := filepath.Join(dir, "high.json")

	// Same server name "x" in both; "y" only in low.
	require.NoError(t, os.WriteFile(low, []byte(
		`{"mcpServers":{"x":{"command":"old"},"y":{"command":"keepme"}}}`), 0644))
	require.NoError(t, os.WriteFile(high, []byte(
		`{"mcpServers":{"x":{"command":"new","enabled":true}}}`), 0644))

	cfg, err := LoadMerged([]string{low, high})
	require.NoError(t, err)

	// "x" comes from the higher-precedence file.
	assert.Equal(t, "new", cfg.Servers["x"].Command)
	assert.True(t, cfg.Servers["x"].Enabled)
	assert.Equal(t, high, cfg.Servers["x"].Origin)
	// "y" survives from the lower file.
	assert.Equal(t, "keepme", cfg.Servers["y"].Command)
	assert.Equal(t, low, cfg.Servers["y"].Origin)
	// Default transport still applied.
	assert.Equal(t, "stdio", cfg.Servers["x"].Transport)
}

func TestLoadMerged_EmptyPaths(t *testing.T) {
	cfg, err := LoadMerged(nil)
	require.NoError(t, err)
	assert.Empty(t, cfg.Servers)
}

func TestNewManagerMulti_SkipsDisabledAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mcp.json")
	// Two servers, neither enabled -> Start connects nothing, no spawn.
	require.NoError(t, os.WriteFile(p, []byte(
		`{"mcpServers":{"a":{"command":"nope"},"b":{"enabled":false,"command":"nope"}}}`), 0644))

	registry := tools.NewRegistry()
	mgr := NewManagerMulti([]string{p}, registry)

	require.NoError(t, mgr.Start(context.Background()))
	assert.Empty(t, mgr.ServerStatus())
	assert.Equal(t, 0, registry.Count())
}
