package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetModeErrorPointsToMigrationGuide(t *testing.T) {
	assert.NoError(t, setModeError(""))
	err := setModeError("claw")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--set-mode was removed in celeste 2.0")
	assert.Contains(t, err.Error(), "MIGRATING-2.0.md")
}

func TestResolveMaxIterFlags(t *testing.T) {
	var warn bytes.Buffer
	n, err := resolveMaxIterFlags(-1, -1, &warn)
	assert.NoError(t, err)
	assert.Equal(t, -1, n)
	assert.Empty(t, warn.String())

	n, err = resolveMaxIterFlags(15, -1, &warn)
	assert.NoError(t, err)
	assert.Equal(t, 15, n)

	n, err = resolveMaxIterFlags(-1, 9, &warn)
	assert.NoError(t, err)
	assert.Equal(t, 9, n)
	assert.Contains(t, warn.String(), "--set-claw-max-iterations is deprecated; use --set-max-tool-iterations")

	warn.Reset()
	n, _ = resolveMaxIterFlags(20, 9, &warn)
	assert.Equal(t, 20, n, "the new flag wins")

	_, err = resolveMaxIterFlags(0, -1, &warn)
	assert.Error(t, err)
	_, err = resolveMaxIterFlags(-1, 0, &warn)
	assert.Error(t, err)
}

// M5: the error must name the flag the user actually typed, not always
// --set-max-tool-iterations.
func TestResolveMaxIterFlagsNamesTheFlagTheUserTyped(t *testing.T) {
	var warn bytes.Buffer

	_, err := resolveMaxIterFlags(0, -1, &warn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--set-max-tool-iterations must be greater than zero")

	_, err = resolveMaxIterFlags(-1, 0, &warn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--set-claw-max-iterations must be greater than zero")
}

// M5: a negative value (typed by the user; -1 alone means "not given") must
// be rejected with an error, not silently treated as "not given" and
// ignored — that let --set-max-tool-iterations -5 do nothing at all.
func TestResolveMaxIterFlagsRejectsNegativeValues(t *testing.T) {
	var warn bytes.Buffer

	_, err := resolveMaxIterFlags(-5, -1, &warn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--set-max-tool-iterations must be greater than zero")

	_, err = resolveMaxIterFlags(-1, -5, &warn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--set-claw-max-iterations must be greater than zero")
}

func TestRemovedTemplatesError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// createConfigTemplate writes into ~/.celeste but does not create it.
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".celeste"), 0o700))
	for _, name := range []string{"celeste-classic", "celeste-claw", "CELESTE-CLAW"} {
		err := createConfigTemplate(name)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "removed in celeste 2.0")
	}
	_, statErr := os.Stat(filepath.Join(home, ".celeste", "config.celeste-claw.json"))
	assert.True(t, os.IsNotExist(statErr), "no profile may be written")
	require.NoError(t, createConfigTemplate("openai"), "live templates still work")
}

func TestSkipPersonaErrorPointsToMigrationGuide(t *testing.T) {
	assert.NoError(t, skipPersonaError(""))
	err := skipPersonaError("true")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--skip-persona was removed in celeste 2.0")
	assert.Contains(t, err.Error(), "MIGRATING-2.0.md")
}

// config --init checks the profile name the same way LoadNamed does, before
// the template lookup, so a name with a separator or a dot segment gets the
// same "invalid profile name" error everywhere.
func TestCreateConfigTemplateRejectsInvalidProfileName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".celeste"), 0o700))
	for _, name := range []string{"../x", "a/b", `a\b`, ".."} {
		err := createConfigTemplate(name)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "invalid profile name", name)
	}
}
