package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// timingSetup isolates HOME (checkpoints and protected files live there)
// and returns a workspace and its store.
func timingSetup(t *testing.T) (string, *checkpoints.SnapshotManager) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return t.TempDir(), checkpoints.NewSnapshotManager("timing-test")
}

// failWritesAfter lets the tools' first ok writes through and makes every
// later one write "partial" and fail, until the test ends (ruling 7).
func failWritesAfter(t *testing.T, ok int) {
	t.Helper()
	orig := writeFileFunc
	n := 0
	writeFileFunc = func(name string, data []byte, perm os.FileMode) error {
		n++
		if n <= ok {
			return orig(name, data, perm)
		}
		_ = orig(name, []byte("partial"), perm)
		return errors.New("disk full")
	}
	t.Cleanup(func() { writeFileFunc = orig })
}

func put(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func get(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func run(t *testing.T, tool tools.Tool, ctx context.Context, input map[string]any) tools.ToolResult {
	t.Helper()
	res, err := tool.Execute(ctx, input, nil)
	require.NoError(t, err)
	return res
}

// Spec acceptance, Review Focus 2: no index entry for a validation failure.
func TestWriteToolsValidationFailuresRecordNothing(t *testing.T) {
	ws, sm := timingSetup(t)
	put(t, filepath.Join(ws, "a.txt"), "hello world hello\n")
	write := NewWriteFileTool(ws, WithWriteFileSnapshots(sm))
	patch := NewPatchFileTool(ws, WithPatchFileSnapshots(sm))
	splice := NewSpliceFileTool(ws, WithSpliceFileSnapshots(sm))
	ctx := context.Background()

	for name, res := range map[string]tools.ToolResult{
		"write outside the workspace": run(t, write, ctx, map[string]any{"path": "../outside.txt", "content": "x"}),
		"patch, old_string missing":   run(t, patch, ctx, map[string]any{"path": "a.txt", "old_string": "absent", "new_string": "x"}),
		"patch, old_string ambiguous": run(t, patch, ctx, map[string]any{"path": "a.txt", "old_string": "hello", "new_string": "x"}),
		"patch, no such file":         run(t, patch, ctx, map[string]any{"path": "missing.txt", "old_string": "a", "new_string": "b"}),
		"splice, bad line range":      run(t, splice, ctx, map[string]any{"op": "move", "source": "a.txt", "dest": "b.txt", "start_line": 9, "end_line": 9}),
	} {
		assert.True(t, res.Error, name)
	}
	assert.Empty(t, sm.Entries())
	_, err := os.Stat(filepath.Join(sm.Dir(), "index.json"))
	assert.True(t, os.IsNotExist(err), "no index was written")
}

func TestWriteFileRecordsTheCallID(t *testing.T) {
	ws, sm := timingSetup(t)
	write := NewWriteFileTool(ws, WithWriteFileSnapshots(sm))
	res := run(t, write, tools.WithCallID(context.Background(), "call_7"), map[string]any{"path": "a.txt", "content": "x"})
	require.False(t, res.Error, res.Content)
	entries := sm.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "call_7", entries[0].MessageID)
	assert.Equal(t, filepath.Join(ws, "a.txt"), entries[0].Path)
	assert.Equal(t, "", entries[0].Backup, "the file was new")
}

// Review Focus 1: a write that fails after the checkpoint leaves the file
// as it was and records nothing.
func TestWriteFileFailedWriteRestoresAndRecordsNothing(t *testing.T) {
	ws, sm := timingSetup(t)
	existing := filepath.Join(ws, "a.txt")
	put(t, existing, "original")
	failWritesAfter(t, 0)
	write := NewWriteFileTool(ws, WithWriteFileSnapshots(sm))

	res := run(t, write, context.Background(), map[string]any{"path": "a.txt", "content": "new"})
	assert.True(t, res.Error)
	assert.Contains(t, res.Content, "disk full")
	assert.Equal(t, "original", get(t, existing))

	res = run(t, write, context.Background(), map[string]any{"path": filepath.Join("sub", "new.txt"), "content": "new"})
	assert.True(t, res.Error)
	_, err := os.Stat(filepath.Join(ws, "sub"))
	assert.True(t, os.IsNotExist(err), "the created directory and partial file are gone")

	assert.Empty(t, sm.Entries())
}

func TestPatchFileFailedWriteRestoresAndRecordsNothing(t *testing.T) {
	ws, sm := timingSetup(t)
	f := filepath.Join(ws, "a.txt")
	put(t, f, "one two three")
	failWritesAfter(t, 0)
	patch := NewPatchFileTool(ws, WithPatchFileSnapshots(sm))
	res := run(t, patch, context.Background(), map[string]any{"path": "a.txt", "old_string": "two", "new_string": "2"})
	assert.True(t, res.Error)
	assert.Equal(t, "one two three", get(t, f))
	assert.Empty(t, sm.Entries())
}

func TestPatchFileSuccessRecordsOneEntry(t *testing.T) {
	ws, sm := timingSetup(t)
	f := filepath.Join(ws, "a.txt")
	put(t, f, "one two three")
	patch := NewPatchFileTool(ws, WithPatchFileSnapshots(sm))
	res := run(t, patch, tools.WithCallID(context.Background(), "call_1"), map[string]any{"path": "a.txt", "old_string": "two", "new_string": "2"})
	require.False(t, res.Error, res.Content)
	assert.Equal(t, "one 2 three", get(t, f))
	entries := sm.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "call_1", entries[0].MessageID)
	_, err := sm.RevertLast()
	require.NoError(t, err)
	assert.Equal(t, "one two three", get(t, f))
}

// A cross-file move whose second write (the source) fails puts both files
// back.
func TestSpliceFileFailedWriteRestoresBothFiles(t *testing.T) {
	ws, sm := timingSetup(t)
	src, dst := filepath.Join(ws, "a.txt"), filepath.Join(ws, "b.txt")
	put(t, src, "a\nb\nc\n")
	put(t, dst, "x\n")
	failWritesAfter(t, 1) // dest is written, then the source write fails
	splice := NewSpliceFileTool(ws, WithSpliceFileSnapshots(sm))
	res := run(t, splice, context.Background(), map[string]any{"op": "move", "source": "a.txt", "dest": "b.txt", "start_line": 2, "end_line": 2})
	assert.True(t, res.Error)
	assert.Equal(t, "a\nb\nc\n", get(t, src))
	assert.Equal(t, "x\n", get(t, dst))
	assert.Empty(t, sm.Entries())
}

// A copy changes only its destination, so only the destination is
// recorded (ruling 6).
func TestSpliceFileCopyRecordsOnlyTheDestination(t *testing.T) {
	ws, sm := timingSetup(t)
	put(t, filepath.Join(ws, "a.txt"), "a\nb\nc\n")
	put(t, filepath.Join(ws, "b.txt"), "x\n")
	splice := NewSpliceFileTool(ws, WithSpliceFileSnapshots(sm))
	res := run(t, splice, context.Background(), map[string]any{"op": "copy", "source": "a.txt", "dest": "b.txt", "start_line": 2, "end_line": 2})
	require.False(t, res.Error, res.Content)
	assert.Equal(t, []string{filepath.Join(ws, "b.txt")}, sm.Files())
}

// One file under two names (a hard link here; letter case on Windows and
// macOS) is one file: a move between them reorders it instead of writing
// it twice and losing the region.
func TestSpliceFileMoveBetweenTwoNamesOfOneFile(t *testing.T) {
	ws, sm := timingSetup(t)
	a := filepath.Join(ws, "a.txt")
	put(t, a, "1\n2\n3\n")
	if err := os.Link(a, filepath.Join(ws, "b.txt")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	splice := NewSpliceFileTool(ws, WithSpliceFileSnapshots(sm))
	res := run(t, splice, context.Background(), map[string]any{"op": "move", "source": "a.txt", "dest": "b.txt", "start_line": 1, "end_line": 1})
	require.False(t, res.Error, res.Content)
	assert.Equal(t, "2\n3\n1\n", get(t, a))
	assert.Len(t, sm.Entries(), 1, "one file, one checkpoint")
}
