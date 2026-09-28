package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAudioProjectCreateResolvesProjectFileInsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	tool := NewAudioProjectTool(workspace)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "create",
		"name":   "test project",
		"output": "final.mp3",
		"tracks": []any{
			map[string]any{
				"file":  "voice.mp3",
				"role":  "voice",
				"start": float64(0),
			},
		},
	}, nil)
	require.NoError(t, err)
	require.False(t, result.Error, result.Content)

	projectFile := filepath.Join(workspace, "final.project.json")
	_, err = os.Stat(projectFile)
	require.NoError(t, err)
	assert.Equal(t, projectFile, result.Metadata["project_file"])
	assert.Contains(t, result.Content, "Project created: "+projectFile)
	assert.Contains(t, result.Content, "file='"+projectFile+"'")
}

func TestAudioProjectCreateRejectsEscapingProjectFile(t *testing.T) {
	workspace := t.TempDir()
	tool := NewAudioProjectTool(workspace)
	escapedProjectFile := filepath.Clean(filepath.Join(workspace, "..", "evil.project.json"))

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "create",
		"name":   "test project",
		"output": "../evil.mp3",
		"tracks": []any{
			map[string]any{
				"file":  "voice.mp3",
				"role":  "voice",
				"start": float64(0),
			},
		},
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "escapes workspace")
	if _, err := os.Stat(escapedProjectFile); !os.IsNotExist(err) {
		t.Fatalf("escaped project file stat err = %v, want not exist", err)
	}
}
