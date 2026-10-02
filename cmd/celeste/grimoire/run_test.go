package grimoire

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// RunInit is /init and celeste init (cleanup-5 D3): .grimoire, then with
// agents AGENTS.md; an existing file is left alone and reported.
func TestRunInit(t *testing.T) {
	isolateHome(t)
	ws := t.TempDir()

	lines, err := RunInit(ws, false)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(ws, ".grimoire"))
	assert.NoFileExists(t, filepath.Join(ws, "AGENTS.md"))
	assert.Contains(t, lines[0], "Created ")
	assert.Contains(t, lines[len(lines)-1], "next session")
	assert.Contains(t, lines[len(lines)-1], "Edit the new file ")

	lines, err = RunInit(ws, true)
	require.NoError(t, err, "an existing .grimoire must not stop agents")
	assert.FileExists(t, filepath.Join(ws, "AGENTS.md"))
	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "already exists")
	assert.Contains(t, joined, "(left as it is)")
	assert.Contains(t, joined, "AGENTS.md")

	lines, err = RunInit(ws, true)
	require.Error(t, err, "nothing left to write is an error")
	assert.True(t, errors.Is(err, fs.ErrExist), "the error says why: %v", err)
	assert.Len(t, lines, 2)
	assert.NotContains(t, strings.Join(lines, "\n"), "next session")
}

// With agents in an empty dir both files are new, and the closing line
// says "files".
func TestRunInitPluralClosingLine(t *testing.T) {
	isolateHome(t)
	ws := t.TempDir()
	lines, err := RunInit(ws, true)
	require.NoError(t, err)
	assert.Contains(t, lines[len(lines)-1], "Edit the new files ")
}

// A step that fails for another reason stops the run with that error.
func TestRunInitStopsOnFailure(t *testing.T) {
	isolateHome(t)
	ws := filepath.Join(t.TempDir(), "missing")
	lines, err := RunInit(ws, true)
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrExist))
	assert.Empty(t, lines)
}

// Describe is /grimoire and celeste grimoire (cleanup-5 D3): warnings,
// sources, the grimoire, then the context files.
func TestDescribe(t *testing.T) {
	isolateHome(t)
	ws := t.TempDir()

	text, found := Describe(ws)
	assert.False(t, found)
	assert.Contains(t, text, "No .grimoire, AGENTS.md or CLAUDE.md found")

	require.NoError(t, os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte(strings.Repeat("a", 40<<10)), 0o644))
	text, found = Describe(ws)
	assert.True(t, found, "an AGENTS.md alone is project context")
	assert.Contains(t, text, "⚠ ")
	assert.Contains(t, text, "cut to")
	assert.Contains(t, text, "Sources:")
	assert.Contains(t, text, "# Project instructions")

	require.NoError(t, os.WriteFile(filepath.Join(ws, ".grimoire"), []byte("## Bindings\n- use tabs\n"), 0o644))
	text, found = Describe(ws)
	assert.True(t, found)
	assert.Contains(t, text, "use tabs")
	assert.Less(t, strings.Index(text, "use tabs"), strings.Index(text, "# Project instructions"),
		"the grimoire comes before the context files")
}
