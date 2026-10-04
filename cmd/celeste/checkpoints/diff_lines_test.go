package checkpoints

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// V20: the empty string after a file's final newline is not a line, so a
// new one-line file "hello\n" is +1, not +2.
func TestSplitLinesCountsLinesNotNewlines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"\n", 1},
		{"hello", 1},
		{"hello\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
		{"a\n\n", 2},
	}
	for _, c := range cases {
		assert.Len(t, splitLines(c.in), c.want, "splitLines(%q)", c.in)
	}
}

func TestComputeDiffCountsTrailingNewlineFiles(t *testing.T) {
	cases := []struct {
		name          string
		before, after *string // nil: the file does not exist
		ins, del      int
	}{
		{"new one-line file", nil, ptr("hello\n"), 1, 0},
		{"new file without trailing newline", nil, ptr("hello"), 1, 0},
		{"new empty file", nil, ptr(""), 0, 0},
		{"appended line", ptr("a\n"), ptr("a\nb\n"), 1, 0},
		{"changed line", ptr("a\nb\n"), ptr("a\nc\n"), 1, 1},
		{"deleted two-line file", ptr("a\nb\n"), nil, 0, 2},
		// Only the final newline changes: git counts the last line as
		// changed, +1 -1, not +0 -0.
		{"final newline added", ptr("a\nb"), ptr("a\nb\n"), 1, 1},
		{"final newline removed", ptr("a\nb\n"), ptr("a\nb"), 1, 1},
		{"unchanged without final newline", ptr("a\nb"), ptr("a\nb"), 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			sm := newSnapshotManagerWithBase(filepath.Join(dir, "backups"))
			f := filepath.Join(dir, "notes.txt")
			if c.before != nil {
				require.NoError(t, os.WriteFile(f, []byte(*c.before), 0o644))
			}
			require.NoError(t, snap(sm, f))
			if c.after != nil {
				require.NoError(t, os.WriteFile(f, []byte(*c.after), 0o644))
			} else {
				require.NoError(t, os.Remove(f))
			}
			changes, err := sm.ComputeDiff()
			require.NoError(t, err)
			require.Len(t, changes, 1)
			assert.Equal(t, c.ins, changes[0].Insertions, "insertions")
			assert.Equal(t, c.del, changes[0].Deletions, "deletions")
		})
	}
}

func ptr(s string) *string { return &s }
