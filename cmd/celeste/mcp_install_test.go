package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertJSONConfig_MergesPreservingOthers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	require.NoError(t, os.WriteFile(p, []byte(
		`{"theme":"dark","mcpServers":{"other":{"command":"x"}}}`), 0o644))

	entry := map[string]any{"command": "/abs/celeste", "args": []string{"serve"}}
	status, err := upsertJSONConfig(p, "celeste", entry, false)
	require.NoError(t, err)
	assert.Equal(t, "updated", status)

	// Backup written.
	_, err = os.Stat(p + ".bak")
	require.NoError(t, err)

	var doc map[string]any
	data, _ := os.ReadFile(p)
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Equal(t, "dark", doc["theme"]) // unrelated top-level key preserved
	servers := doc["mcpServers"].(map[string]any)
	assert.Contains(t, servers, "other")   // other server preserved
	assert.Contains(t, servers, "celeste") // ours added
	cel := servers["celeste"].(map[string]any)
	assert.Equal(t, "/abs/celeste", cel["command"])
}

func TestUpsertJSONConfig_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "cfg.json") // parent dir does not exist yet
	status, err := upsertJSONConfig(p, "celeste", map[string]any{"command": "/c"}, false)
	require.NoError(t, err)
	assert.Equal(t, "created", status)
	_, err = os.Stat(p)
	require.NoError(t, err)
}

func TestUpsertJSONConfig_RefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	require.NoError(t, os.WriteFile(real, []byte(`{}`), 0o644))
	link := filepath.Join(dir, "link.json")
	require.NoError(t, os.Symlink(real, link))

	_, err := upsertJSONConfig(link, "celeste", map[string]any{"command": "/c"}, false)
	assert.Error(t, err)
}

func TestUpsertJSONConfig_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	status, err := upsertJSONConfig(p, "celeste", map[string]any{"command": "/c"}, true)
	require.NoError(t, err)
	assert.Contains(t, status, "dry-run")
	_, err = os.Stat(p)
	assert.True(t, os.IsNotExist(err))
}
