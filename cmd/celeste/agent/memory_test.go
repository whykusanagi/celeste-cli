package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectMemoryStoreAppendLoadAndRecall(t *testing.T) {
	base := t.TempDir()
	store, err := NewProjectMemoryStore(base)
	require.NoError(t, err)

	workspace := filepath.Join(base, "workspace")
	require.NoError(t, os.MkdirAll(workspace, 0755))

	_, err = store.Append(workspace, []MemoryEntry{
		{
			Category: "run_summary",
			Content:  "Fixed flaky go test behavior in providers package and added regression checks",
			Goal:     "fix flaky tests",
		},
		{
			Category: "blocker",
			Content:  "Missing OPENAI_API_KEY for provider integration checks",
			Goal:     "run full integration tests",
		},
	}, 50)
	require.NoError(t, err)

	loaded, err := store.Load(workspace)
	require.NoError(t, err)
	require.Len(t, loaded.Entries, 2)

	recall := loaded.Recall("fix providers go test", 2)
	require.NotEmpty(t, recall)
	assert.Equal(t, "run_summary", recall[0].Category)
	assert.Contains(t, recall[0].Content, "providers package")
}

func TestProjectMemoryStoreAppendDedupesAndTrims(t *testing.T) {
	base := t.TempDir()
	store, err := NewProjectMemoryStore(base)
	require.NoError(t, err)

	workspace := filepath.Join(base, "workspace")
	require.NoError(t, os.MkdirAll(workspace, 0755))

	_, err = store.Append(workspace, []MemoryEntry{
		{Category: "run_summary", Content: "Initial run summary", Status: StatusCompleted},
		{Category: "failure", Content: "Verification failed in CI", Status: StatusVerificationStop},
	}, 10)
	require.NoError(t, err)

	_, err = store.Append(workspace, []MemoryEntry{
		{Category: "run_summary", Content: "Initial run summary", Status: StatusFailed},
		{Category: "blocker", Content: "Need user approval for risky action", Status: StatusBlocked},
	}, 2)
	require.NoError(t, err)

	loaded, err := store.Load(workspace)
	require.NoError(t, err)
	require.Len(t, loaded.Entries, 2)

	runSummaryCount := 0
	for _, entry := range loaded.Entries {
		if entry.Category == "run_summary" {
			runSummaryCount++
			assert.Equal(t, StatusFailed, entry.Status)
		}
	}
	assert.Equal(t, 1, runSummaryCount, "duplicate run_summary entries should be merged")
}
