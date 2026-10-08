//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpsertJSONConfig_OwnerOnlyModes: the .bak of a client config (which
// can hold API keys in env) and a newly created config are readable only by
// the owner, whatever the umask, and an existing world-readable .bak is
// tightened (Aikido 806869435).
func TestUpsertJSONConfig_OwnerOnlyModes(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)

	dir := t.TempDir()
	path := filepath.Join(dir, "client.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0o600))
	require.NoError(t, os.WriteFile(path+".bak", []byte("old"), 0o644))
	_, err := upsertJSONConfig(path, "celeste", map[string]any{"command": "x"}, false)
	require.NoError(t, err)
	fi, err := os.Stat(path + ".bak")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "backup mode")

	fresh := filepath.Join(dir, "new.json")
	_, err = upsertJSONConfig(fresh, "celeste", map[string]any{"command": "x"}, false)
	require.NoError(t, err)
	fi, err = os.Stat(fresh)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "new config mode")
}
