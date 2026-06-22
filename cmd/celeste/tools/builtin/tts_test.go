package builtin

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTTSResolveOutput verifies generated audio resolves to the workspace:
// relative names join the workspace, absolute names pass through, and with no
// workspace the name is unchanged (legacy cwd behavior).
func TestTTSResolveOutput(t *testing.T) {
	// Build paths with filepath so the test is correct on Windows too (separators
	// and IsAbs semantics differ from Unix).
	wsDir := filepath.Join(t.TempDir(), "work")
	ws := NewTTSTool(wsDir)
	assert.Equal(t, filepath.Join(wsDir, "speech_1.mp3"), ws.resolveOutput("speech_1.mp3"), "relative name should join the workspace")

	abs := filepath.Join(t.TempDir(), "out.mp3") // t.TempDir() is absolute on every OS
	assert.Equal(t, abs, ws.resolveOutput(abs), "absolute name should pass through unchanged")

	none := NewTTSTool("")
	assert.Equal(t, "speech_1.mp3", none.resolveOutput("speech_1.mp3"), "no workspace should leave the name unchanged")
}

// TestTTSSpeakTextValidation verifies that a missing "text" field and an
// empty "text" string produce DIFFERENT error messages, so callers can
// distinguish transit corruption from a genuinely empty argument.
func TestTTSSpeakTextValidation(t *testing.T) {
	tool := NewTTSTool("")

	// Provide a dummy API key so we reach the text-validation block.
	// The key is fake — we expect the error before any HTTP call is made.
	t.Setenv("ELEVEN_LABS_API_KEY", "dummy-key-for-testing")

	t.Run("missing text field", func(t *testing.T) {
		input := map[string]any{
			"action": "speak",
			// "text" intentionally absent — simulates a dropped stream delta
		}
		result, err := tool.Execute(context.Background(), input, nil)
		require.NoError(t, err)
		assert.True(t, result.Error)
		assert.Contains(t, result.Content, "corrupted in transit")
		assert.NotContains(t, result.Content, "empty string")
	})

	t.Run("empty text string", func(t *testing.T) {
		input := map[string]any{
			"action": "speak",
			"text":   "",
		}
		result, err := tool.Execute(context.Background(), input, nil)
		require.NoError(t, err)
		assert.True(t, result.Error)
		assert.Contains(t, result.Content, "empty string")
		assert.NotContains(t, result.Content, "corrupted in transit")
	})

	t.Run("whitespace-only text string", func(t *testing.T) {
		input := map[string]any{
			"action": "speak",
			"text":   "   ",
		}
		result, err := tool.Execute(context.Background(), input, nil)
		require.NoError(t, err)
		assert.True(t, result.Error)
		assert.Contains(t, result.Content, "empty string")
	})
}
