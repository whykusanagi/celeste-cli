package config

import (
	"bytes"
	"embed"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures are embedded so the prebuilt test binary also runs them in
// the Docker job, which has no repo checkout next to it.
//
//go:embed testdata/migrate/*.json
var migrateFixtures embed.FS

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := migrateFixtures.ReadFile("testdata/migrate/" + name)
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

// M7: a non-number claw_max_tool_iterations must not be moved or renamed —
// doing so would make the resulting parse error (MaxToolIterations is an
// int) name max_tool_iterations instead of the key the user actually wrote.
func TestMigrateLegacyKeysLeavesNonNumberClawIterUntouched(t *testing.T) {
	in := fixture(t, "non_number_claw_iter.json")
	out, notes, changed := migrateLegacyKeys(in)
	require.True(t, changed, "runtime_mode alone still triggers a migration")
	assert.Len(t, notes, 1, "only the runtime_mode removal, not a claw_max_tool_iterations rename")

	after := compactValues(t, out)
	assert.NotContains(t, after, "runtime_mode")
	assert.Equal(t, `"seven"`, after["claw_max_tool_iterations"], "a non-number value stays under its original key")
	assert.NotContains(t, after, "max_tool_iterations", "must not be moved when it isn't a number")
}

// M7: when the only legacy key present is a non-number
// claw_max_tool_iterations, there is nothing safe to migrate — the file is
// left completely untouched.
func TestMigrateLegacyKeysNonNumberClawIterAloneIsNoOp(t *testing.T) {
	in := fixture(t, "only_non_number_claw_iter.json")
	out, notes, changed := migrateLegacyKeys(in)
	assert.False(t, changed)
	assert.Empty(t, notes)
	assert.Equal(t, in, out)
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

// I3: a dotfile-manager-style symlinked config must still migrate. Before
// the fix, the atomic write renamed the temp file onto the symlink
// path itself, replacing the symlink with a plain file and leaving the
// dotfile manager's real target unmigrated forever.
func TestMigrateFileFollowsSymlink(t *testing.T) {
	realDir := t.TempDir()
	real := filepath.Join(realDir, "config.json")
	require.NoError(t, os.WriteFile(real, fixture(t, "legacy.json"), 0o600))

	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "config.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("could not create a symlink (likely Windows without Developer Mode): %v", err)
	}

	orig := MigrationWarn
	MigrationWarn = func(string) {}
	t.Cleanup(func() { MigrationWarn = orig })

	got := migrateFile(link, fixture(t, "legacy.json"))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.True(t, info.Mode()&os.ModeSymlink != 0, "migration must not replace the symlink with a regular file")

	onDisk, err := os.ReadFile(real)
	require.NoError(t, err)
	assert.Equal(t, got, onDisk)
	assert.NotContains(t, string(onDisk), "runtime_mode")
}

// Review finding I1(c): once a path's save has failed, later calls for that
// same path must not repeat the warning or retry the write — both are
// useless (the on-disk file still has the legacy keys, so every load would
// otherwise re-warn and re-attempt the save forever).
func TestMigrateFileRemembersFailedSaveDoesNotRewarnOrRetry(t *testing.T) {
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

	migrateFile(path, fixture(t, "legacy.json"))
	require.NotEmpty(t, warned, "the first call must warn")
	firstCount := len(warned)

	migrateFile(path, fixture(t, "legacy.json"))
	assert.Len(t, warned, firstCount, "a second call for the same known-bad path must not warn again")
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

// I1(a): the chat startup path migrates every profile in ~/.celeste once,
// before the alt screen opens, so a later /endpoint or SwitchEndpoint load
// of a different profile (while the TUI is already running) is a no-op
// instead of printing migration notes into the alt screen.
func TestMigrateConfigDirMigratesEveryProfileOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var warned []string
	orig := MigrationWarn
	MigrationWarn = func(s string) { warned = append(warned, s) }
	t.Cleanup(func() { MigrationWarn = orig })

	celesteDir := filepath.Join(home, ".celeste")
	require.NoError(t, os.MkdirAll(celesteDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(celesteDir, "config.json"), fixture(t, "legacy.json"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(celesteDir, "config.work.json"), fixture(t, "legacy.json"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(celesteDir, "config.clean.json"), fixture(t, "clean.json"), 0o600))

	MigrateConfigDir()
	require.NotEmpty(t, warned, "migrating two legacy profiles must warn")

	for _, name := range []string{"config.json", "config.work.json"} {
		onDisk, err := os.ReadFile(filepath.Join(celesteDir, name))
		require.NoError(t, err)
		assert.NotContains(t, string(onDisk), "runtime_mode", name)
	}
	cleanOnDisk, err := os.ReadFile(filepath.Join(celesteDir, "config.clean.json"))
	require.NoError(t, err)
	assert.Equal(t, fixture(t, "clean.json"), cleanOnDisk, "a clean profile is untouched")

	// A later LoadNamed for an already-migrated profile finds nothing left
	// to do: no repeated warning.
	warned = nil
	_, err = LoadNamed("work")
	require.NoError(t, err)
	assert.Empty(t, warned)
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
