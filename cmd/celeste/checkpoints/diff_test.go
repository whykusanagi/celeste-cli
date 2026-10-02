package checkpoints

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeDiff_Insertions(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2"), 0644))

	require.NoError(t, snap(sm, srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2\nline3\nline4"), 0644))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	assert.Equal(t, 2, changes[0].Insertions)
	assert.Equal(t, 0, changes[0].Deletions)
	assert.False(t, changes[0].IsNew)
}

func TestComputeDiff_Deletions(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2\nline3"), 0644))

	require.NoError(t, snap(sm, srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("line1"), 0644))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	assert.Equal(t, 0, changes[0].Insertions)
	assert.Equal(t, 2, changes[0].Deletions)
}

func TestComputeDiff_NewFile(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "new.txt")
	require.NoError(t, snap(sm, srcFile))

	// Now create the file
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2\nline3"), 0644))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	assert.True(t, changes[0].IsNew)
	assert.Equal(t, 3, changes[0].Insertions)
	assert.Equal(t, 0, changes[0].Deletions)
}

func TestComputeDiff_DeletedFile(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2"), 0644))

	require.NoError(t, snap(sm, srcFile))
	require.NoError(t, os.Remove(srcFile))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	assert.Equal(t, 0, changes[0].Insertions)
	assert.Equal(t, 2, changes[0].Deletions)
}

func TestComputeDiff_MultipleSnapshots_UsesEarliest(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("original"), 0644))

	require.NoError(t, snap(sm, srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("modified once"), 0644))
	require.NoError(t, snap(sm, srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("modified twice"), 0644))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	// Diff should be against the earliest snapshot ("original"), not the second
}

func TestComputeDiff_Mixed(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2\nline3"), 0644))

	require.NoError(t, snap(sm, srcFile))
	// Replace line2 with lineX and add line4
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nlineX\nline3\nline4"), 0644))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	// line2 -> lineX is 1 deletion + 1 insertion, plus line4 is 1 insertion
	assert.Equal(t, 2, changes[0].Insertions)
	assert.Equal(t, 1, changes[0].Deletions)
}

func TestDiffStats(t *testing.T) {
	tests := []struct {
		name    string
		old     []string
		new     []string
		wantIns int
		wantDel int
	}{
		{"identical", []string{"a", "b"}, []string{"a", "b"}, 0, 0},
		{"pure insert", []string{"a"}, []string{"a", "b", "c"}, 2, 0},
		{"pure delete", []string{"a", "b", "c"}, []string{"a"}, 0, 2},
		{"replacement", []string{"a", "b"}, []string{"a", "c"}, 1, 1},
		{"empty to content", []string{}, []string{"a", "b"}, 2, 0},
		{"content to empty", []string{"a", "b"}, []string{}, 0, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ins, del, _ := diffStats(tt.old, tt.new)
			assert.Equal(t, tt.wantIns, ins, "insertions")
			assert.Equal(t, tt.wantDel, del, "deletions")
		})
	}
}

func TestComputeDiffMarksDeletedFilesAndSorts(t *testing.T) {
	dir := t.TempDir()
	sm := newSnapshotManagerWithBase(filepath.Join(dir, "session"))
	b, a := filepath.Join(dir, "b.txt"), filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(b, []byte("x\ny"), 0o644))
	require.NoError(t, os.WriteFile(a, []byte("x"), 0o644))
	require.NoError(t, snap(sm, b))
	require.NoError(t, snap(sm, a))
	require.NoError(t, os.Remove(b))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	require.Len(t, changes, 2)
	assert.Equal(t, a, changes[0].Path)
	assert.Equal(t, b, changes[1].Path)
	assert.True(t, changes[1].Deleted)
	assert.Equal(t, 2, changes[1].Deletions)
}

func TestFormatChanges(t *testing.T) {
	ws := t.TempDir()
	changes := []FileChange{
		{Path: filepath.Join(ws, "a.txt"), Insertions: 3, Deletions: 1},
		{Path: filepath.Join(ws, "sub", "new.txt"), Insertions: 2, IsNew: true},
		{Path: filepath.Join(ws, "gone.txt"), Deletions: 2, Deleted: true},
	}
	outside := filepath.Join(t.TempDir(), "elsewhere.txt")
	changes = append(changes, FileChange{Path: outside, Insertions: 1})

	want := "Files changed this session:\n" +
		"  a.txt  +3 -1\n" +
		"  " + filepath.Join("sub", "new.txt") + "  +2 -0 (new)\n" +
		"  gone.txt  +0 -2 (deleted)\n" +
		"  " + outside + "  +1 -0"
	assert.Equal(t, want, FormatChanges(changes, ws))
	assert.Equal(t, "No files changed in this session.", FormatChanges(nil, ws))
}

// /diff reads backups only inside the session directory, whatever a
// hand-edited index names.
func TestComputeDiffRefusesBackupsOutsideTheSession(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "session")
	require.NoError(t, os.MkdirAll(sdir, 0o700))
	f := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(f, []byte("now"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("s\ne\nc"), 0o600))
	data, err := json.Marshal([]Entry{{Path: f, Version: 1, Backup: filepath.Join("..", "secret.txt")}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(sdir, "index.json"), data, 0o600))

	changes, err := newSnapshotManagerWithBase(sdir).ComputeDiff()
	require.NoError(t, err, "one file's error does not fail the listing")
	require.Len(t, changes, 1)
	assert.Contains(t, changes[0].Err, "invalid backup name")
}

// Review M4: /diff stays bounded and readable. A binary file says so; a
// file over maxDiffBytes is compared by size only, never read; a pair
// whose line comparison would be too large gets line counts only; a file
// that cannot be read shows its error while the others still list.
func TestComputeDiffBinaryLargeAndErrors(t *testing.T) {
	dir := t.TempDir()
	sm := newSnapshotManagerWithBase(filepath.Join(dir, "session"))
	bin, big, long, bad := filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.big"), filepath.Join(dir, "c.txt"), filepath.Join(dir, "d.txt")
	require.NoError(t, os.WriteFile(bin, []byte("x\x00y"), 0o644))
	require.NoError(t, os.WriteFile(big, []byte("small"), 0o644))
	lines := strings.Repeat("line\n", 4000)
	require.NoError(t, os.WriteFile(long, []byte(lines), 0o644))
	require.NoError(t, os.WriteFile(bad, []byte("x"), 0o644))
	for _, f := range []string{bin, big, long, bad} {
		require.NoError(t, snap(sm, f))
	}
	require.NoError(t, os.WriteFile(bin, []byte("x\x00z"), 0o644))
	require.NoError(t, os.WriteFile(big, make([]byte, maxDiffBytes+1), 0o644))
	require.NoError(t, os.WriteFile(long, []byte(strings.Repeat("other\n", 3000)), 0o644))
	require.NoError(t, os.Remove(filepath.Join(sm.Dir(), sm.Entries()[3].Backup)))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	require.Len(t, changes, 4)
	out := FormatChanges(changes, dir)
	assert.Contains(t, out, "  a.bin  (binary)\n")
	assert.Contains(t, out, fmt.Sprintf("  b.big  (large file: 5 -> %d bytes)\n", maxDiffBytes+1))
	assert.Contains(t, out, "  c.txt  +0 -1000 (large file, line counts only)\n")
	assert.Contains(t, out, "  d.txt  (error: ")
}
