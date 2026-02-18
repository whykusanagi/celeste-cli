package collections

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

func TestManager_EnableDisableCollection(t *testing.T) {
	cfg := &config.Config{
		Collections: &config.CollectionsConfig{
			Enabled:           true,
			ActiveCollections: []string{},
			AutoEnable:        true,
		},
	}

	// Create mock client (nil is fine for this test)
	manager := NewManager(nil, cfg)

	// Enable collection
	err := manager.EnableCollection("col_123")
	require.NoError(t, err)
	assert.Contains(t, cfg.Collections.ActiveCollections, "col_123")

	// Disable collection
	err = manager.DisableCollection("col_123")
	require.NoError(t, err)
	assert.NotContains(t, cfg.Collections.ActiveCollections, "col_123")
}

func TestManager_ValidateDocument(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		filename    string
		content     string
		size        int64
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "valid markdown",
			filename:    "test.md",
			content:     "# Test",
			size:        6,
			shouldError: false,
		},
		{
			name:        "valid txt",
			filename:    "test.txt",
			content:     "Test",
			size:        4,
			shouldError: false,
		},
		{
			name:        "unsupported format",
			filename:    "test.exe",
			content:     "data",
			size:        4,
			shouldError: true,
			errorMsg:    "unsupported format",
		},
		{
			name:        "file too large",
			filename:    "large.md",
			content:     "",
			size:        11 * 1024 * 1024, // 11MB
			shouldError: true,
			errorMsg:    "file too large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			path := filepath.Join(tmpDir, tt.filename)
			if tt.size > 0 {
				// For size test, create actual large file
				f, err := os.Create(path)
				require.NoError(t, err)
				err = f.Truncate(tt.size)
				require.NoError(t, err)
				f.Close()
			} else {
				os.WriteFile(path, []byte(tt.content), 0644)
			}

			// Test validation
			err := ValidateDocument(path)

			if tt.shouldError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}

			// Cleanup
			os.Remove(path)
		})
	}
}
