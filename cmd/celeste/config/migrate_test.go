package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "migrate", name))
	require.NoError(t, err)
	return b
}

// compactValues decodes a JSON object into key -> compacted value bytes.
func compactValues(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	out := map[string]string{}
	for k, v := range raw {
		var b bytes.Buffer
		require.NoError(t, json.Compact(&b, v))
		out[k] = b.String()
	}
	return out
}

// Spec §5 W6: removed keys disappear, renamed keys map, every other key is
// preserved byte-for-byte in value.
func TestMigrateLegacyKeysFixture(t *testing.T) {
	in := fixture(t, "legacy.json")
	out, notes, changed := migrateLegacyKeys(in)
	require.True(t, changed)
	assert.Len(t, notes, 2)

	before, after := compactValues(t, in), compactValues(t, out)
	assert.NotContains(t, after, "runtime_mode")
	assert.NotContains(t, after, "claw_max_tool_iterations")
	assert.Equal(t, "7", after["max_tool_iterations"])
	for k, v := range before {
		if k == "runtime_mode" || k == "claw_max_tool_iterations" {
			continue
		}
		assert.Equal(t, v, after[k], "value of %q changed", k)
	}
	assert.Len(t, after, len(before)-1, "one key removed, one renamed")
	// base_url legitimately contains a raw "&"; the byte-preservation loop
	// above already proves it survives untouched. What HTML-escaping would
	// mangle is turning it into the backslash-u-zero-zero-two-six escape,
	// so that is what this checks (SetEscapeHTML(false)).
	assert.NotContains(t, string(out), "\\u0026", "HTML escaping rewrote a value")
}

func TestMigrateLegacyKeysNewKeyWins(t *testing.T) {
	out, _, changed := migrateLegacyKeys(fixture(t, "both_keys.json"))
	require.True(t, changed)
	after := compactValues(t, out)
	assert.Equal(t, "40", after["max_tool_iterations"])
	assert.NotContains(t, after, "claw_max_tool_iterations")
	assert.NotContains(t, after, "runtime_mode")
}

func TestMigrateLegacyKeysLeavesCleanAndInvalidAlone(t *testing.T) {
	clean := fixture(t, "clean.json")
	out, notes, changed := migrateLegacyKeys(clean)
	assert.False(t, changed)
	assert.Empty(t, notes)
	assert.Equal(t, clean, out)

	for _, bad := range []string{"", "not json", "[1,2]", "null"} {
		out, _, changed := migrateLegacyKeys([]byte(bad))
		assert.False(t, changed, bad)
		assert.Equal(t, []byte(bad), out)
	}
}

// Load -> save: migrateFile saves the migrated bytes in place with the
// file's permissions and warns once per key, naming the file.
func TestMigrateFileSavesAndWarns(t *testing.T) {
	var warned []string
	orig := MigrationWarn
	MigrationWarn = func(s string) { warned = append(warned, s) }
	t.Cleanup(func() { MigrationWarn = orig })

	path := filepath.Join(t.TempDir(), "config.legacy.json")
	require.NoError(t, os.WriteFile(path, fixture(t, "legacy.json"), 0o600))
	got := migrateFile(path, fixture(t, "legacy.json"))

	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, got, onDisk)
	assert.NotContains(t, string(onDisk), "runtime_mode")
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	require.Len(t, warned, 2)
	for _, w := range warned {
		assert.Contains(t, w, path)
		assert.Contains(t, w, "MIGRATING-2.0.md")
	}

	// A second load finds nothing to do.
	warned = nil
	again := migrateFile(path, onDisk)
	assert.Equal(t, onDisk, again)
	assert.Empty(t, warned)
}

// Review focus 5: a config that can't be rewritten still loads migrated.
func TestMigrateFileReadOnlyStillLoads(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not block writes here")
	}
	var warned []string
	orig := MigrationWarn
	MigrationWarn = func(s string) { warned = append(warned, s) }
	t.Cleanup(func() { MigrationWarn = orig })

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, fixture(t, "legacy.json"), 0o444))
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	got := migrateFile(path, fixture(t, "legacy.json"))
	assert.Equal(t, "7", compactValues(t, got)["max_tool_iterations"])
	assert.True(t, strings.Contains(strings.Join(warned, "\n"), "could not save"), warned)
}

// Load -> save through the real loader: a named profile with the legacy keys
// loads with the renamed value, is rewritten on disk, and a later SaveNamed
// never brings the removed keys back.
func TestLoadNamedMigratesLegacyProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	orig := MigrationWarn
	MigrationWarn = func(string) {}
	t.Cleanup(func() { MigrationWarn = orig })
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".celeste"), 0o700))
	path := NamedConfigPath("legacy")
	require.NoError(t, os.WriteFile(path, fixture(t, "legacy.json"), 0o600))

	cfg, err := LoadNamed("legacy")
	require.NoError(t, err)
	assert.Equal(t, 7, cfg.MaxToolIterations)
	assert.Equal(t, "gpt-4.1", cfg.Model)

	onDisk, _ := os.ReadFile(path)
	before, after := compactValues(t, fixture(t, "legacy.json")), compactValues(t, onDisk)
	for k, v := range before {
		if k == "runtime_mode" || k == "claw_max_tool_iterations" {
			continue
		}
		assert.Equal(t, v, after[k], "value of %q changed on disk", k)
	}

	require.NoError(t, SaveNamed("legacy", cfg))
	saved, _ := os.ReadFile(path)
	assert.NotContains(t, string(saved), "runtime_mode")
	assert.NotContains(t, string(saved), "claw_max_tool_iterations")
	assert.Contains(t, string(saved), `"max_tool_iterations": 7`)
}

// The default config.json path (Load) migrates too.
func TestLoadMigratesDefaultConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	orig := MigrationWarn
	MigrationWarn = func(string) {}
	t.Cleanup(func() { MigrationWarn = orig })
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".celeste"), 0o755))
	_, configFile, _, _ := Paths()
	require.NoError(t, os.WriteFile(configFile, fixture(t, "legacy.json"), 0o644))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 7, cfg.MaxToolIterations)
	onDisk, _ := os.ReadFile(configFile)
	assert.NotContains(t, string(onDisk), "runtime_mode")
}

// Review focus 5: permissions.json has its own "mode" key and is never a
// config file; loading a profile leaves it byte-identical.
func TestLoadNamedLeavesPermissionsJSONAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".celeste")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	perms := []byte(`{"mode":"default","rules":[]}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "permissions.json"), perms, 0o600))
	require.NoError(t, os.WriteFile(NamedConfigPath("p"), fixture(t, "legacy.json"), 0o600))
	orig := MigrationWarn
	MigrationWarn = func(string) {}
	t.Cleanup(func() { MigrationWarn = orig })
	_, err := LoadNamed("p")
	require.NoError(t, err)
	got, _ := os.ReadFile(filepath.Join(dir, "permissions.json"))
	assert.Equal(t, perms, got)
}
