//go:build !windows

package mcp

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkspaceConfigDeviceOrFIFONotRead: a workspace .mcp.json that is a
// FIFO, or a link to a device, is neither discovered nor read: reading it
// would block or never end (Aikido 806869859).
func TestWorkspaceConfigDeviceOrFIFONotRead(t *testing.T) {
	home := t.TempDir()

	wsDev := t.TempDir()
	require.NoError(t, os.Symlink("/dev/zero", filepath.Join(wsDev, ".mcp.json")))
	assert.Empty(t, DiscoverConfigPaths(wsDev, home))

	wsFifo := t.TempDir()
	fifo := filepath.Join(wsFifo, ".mcp.json")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	assert.Empty(t, DiscoverConfigPaths(wsFifo, home))

	// LoadConfig refuses them directly too, without blocking.
	for _, p := range []string{filepath.Join(wsDev, ".mcp.json"), fifo} {
		done := make(chan error, 1)
		go func() { _, err := LoadConfig(p); done <- err }()
		select {
		case err := <-done:
			assert.Error(t, err, p)
		case <-time.After(3 * time.Second):
			t.Fatalf("LoadConfig(%s) blocked", p)
		}
	}
}
