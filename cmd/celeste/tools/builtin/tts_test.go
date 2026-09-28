package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTTSResolveOutput(t *testing.T) {
	t.Run("relative filename resolves inside workspace", func(t *testing.T) {
		workspace := t.TempDir()
		got, err := NewTTSTool(workspace).resolveOutput("speech_1.mp3")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(workspace, "speech_1.mp3"), got)
		assert.NotEqual(t, "speech_1.mp3", got)
	})

	t.Run("absolute path outside workspace errors", func(t *testing.T) {
		workspace := t.TempDir()
		outside := filepath.Join(t.TempDir(), "out.mp3")
		_, err := NewTTSTool(workspace).resolveOutput(outside)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes workspace")
	})

	t.Run("traversal errors", func(t *testing.T) {
		workspace := t.TempDir()
		_, err := NewTTSTool(workspace).resolveOutput("../x.mp3")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes workspace")
	})

	t.Run("protected hook file errors", func(t *testing.T) {
		home := setProtectedHome(t)
		_, err := NewTTSTool(home).resolveOutput(".celeste/hooks.json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "protected")
	})

	t.Run("empty workspace relative filename resolves against cwd", func(t *testing.T) {
		_, err := NewTTSTool("").resolveOutput("speech_1.mp3")
		require.NoError(t, err)
	})

	t.Run("empty workspace traversal errors", func(t *testing.T) {
		_, err := NewTTSTool("").resolveOutput("../x.mp3")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes workspace")
	})
}

func TestTTSSpeakRejectsEscapingFilenameBeforeNetwork(t *testing.T) {
	tool := NewTTSTool(t.TempDir())
	t.Setenv("ELEVEN_LABS_API_KEY", "dummy-key-for-testing")

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":   "speak",
		"text":     "hello",
		"voice_id": "voice-id-for-test",
		"filename": "../escape.mp3",
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "escapes workspace")
	assert.False(t, strings.Contains(result.Content, "TTS generation failed"))
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

func TestExecuteBatchRejectsEscapingClipNameBeforeNetwork(t *testing.T) {
	workspace := t.TempDir()
	outDir := filepath.Join(workspace, "out")
	clipsPath := filepath.Join(workspace, "clips.json")
	clips := clipsFile{
		Clips: []struct {
			Name             string   `json:"name"`
			Script           string   `json:"script"`
			DurationEstimate string   `json:"duration_estimate"`
			Tags             []string `json:"tags"`
		}{
			{Name: "../../evil", Script: "hello"},
		},
	}
	data, err := json.Marshal(clips)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(clipsPath, data, 0644))

	result, err := executeBatch(context.Background(), "dummy-api-key", "dummy-voice", clipsPath, outDir, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)
	assert.Contains(t, result.Content, "FAIL")
	assert.Contains(t, result.Content, "../../evil")
	assert.Contains(t, result.Content, "escapes workspace")
	assert.Equal(t, 1, result.Metadata["clips_count"])

	escapedOutput := filepath.Clean(filepath.Join(outDir, "../../evil.mp3"))
	if _, err := os.Stat(escapedOutput); !os.IsNotExist(err) {
		t.Fatalf("escaped batch output stat err = %v, want not exist", err)
	}
}
