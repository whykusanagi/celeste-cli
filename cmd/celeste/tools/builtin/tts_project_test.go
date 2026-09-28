package builtin

import (
	"context"
	"encoding/json"
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

func TestAudioProjectRenderRejectsRelativeEscapingOutputBeforeFFmpeg(t *testing.T) {
	workspace := t.TempDir()
	tool := NewAudioProjectTool(workspace)
	t.Chdir(workspace)

	require.NoError(t, os.WriteFile(filepath.Join(workspace, "voice.mp3"), []byte("dummy audio"), 0644))
	projectPath := filepath.Join(workspace, "project.json")
	project := AudioProject{
		Output: "../evil.mp3",
		Tracks: []AudioTrack{
			{File: "voice.mp3", Role: "voice", Start: 0, Volume: 1},
		},
	}
	data, err := json.Marshal(project)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(projectPath, data, 0644))

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "render",
		"file":   "project.json",
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "escapes workspace")

	escapedOutput := filepath.Clean(filepath.Join(workspace, "..", "evil.mp3"))
	if _, err := os.Stat(escapedOutput); !os.IsNotExist(err) {
		t.Fatalf("escaped output stat err = %v, want not exist", err)
	}
}

func TestAudioProjectRenderRejectsAbsoluteEscapingOutputBeforeFFmpeg(t *testing.T) {
	workspace := t.TempDir()
	tool := NewAudioProjectTool(workspace)
	t.Chdir(workspace)

	require.NoError(t, os.WriteFile(filepath.Join(workspace, "voice.mp3"), []byte("dummy audio"), 0644))
	outsideOutput := filepath.Join(t.TempDir(), "out.mp3")
	projectPath := filepath.Join(workspace, "project.json")
	project := AudioProject{
		Output: outsideOutput,
		Tracks: []AudioTrack{
			{File: "voice.mp3", Role: "voice", Start: 0, Volume: 1},
		},
	}
	data, err := json.Marshal(project)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(projectPath, data, 0644))

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "render",
		"file":   "project.json",
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "escapes workspace")

	if _, err := os.Stat(outsideOutput); !os.IsNotExist(err) {
		t.Fatalf("absolute escaped output stat err = %v, want not exist", err)
	}
}
