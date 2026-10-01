package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Config keys celeste 2.0 removed or renamed (#144, spec §6.2).
const (
	legacyRuntimeModeKey = "runtime_mode"
	legacyClawMaxIterKey = "claw_max_tool_iterations"
	maxToolIterationsKey = "max_tool_iterations"
	migrationGuide       = "see MIGRATING-2.0.md"
)

// MigrationWarn prints the one-line note for each migrated key. Tests
// replace it.
var MigrationWarn = func(msg string) { fmt.Fprintln(os.Stderr, "celeste: "+msg) }

// migrateLegacyKeys rewrites one config file's JSON for 2.0:
//   - runtime_mode is dropped (2.0 has one chat mode);
//   - claw_max_tool_iterations becomes max_tool_iterations, unless the file
//     already sets max_tool_iterations (which wins), or the value isn't a
//     JSON number (left in place so the resulting parse error names the key
//     the user actually wrote, not the renamed one — #144 W6b review, M7).
//
// Every other key keeps its value bytes exactly (json.RawMessage, HTML
// escaping off). Keys come out alphabetically sorted (encoding/json sorts
// map keys) with two-space indentation — a different order than celeste's
// own Save, which follows the Config struct's field order, so a migrated
// file's key order won't match a freshly saved one. changed is false, and
// out is data, when no legacy key is present or data is not a JSON object.
func migrateLegacyKeys(data []byte) (out []byte, notes []string, changed bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return data, nil, false
	}
	if _, ok := raw[legacyRuntimeModeKey]; ok {
		delete(raw, legacyRuntimeModeKey)
		notes = append(notes, "removed runtime_mode: 2.0 has one chat mode, which always runs tools in a loop ("+migrationGuide+")")
	}
	if v, ok := raw[legacyClawMaxIterKey]; ok && isJSONNumber(v) {
		delete(raw, legacyClawMaxIterKey)
		if _, set := raw[maxToolIterationsKey]; !set {
			raw[maxToolIterationsKey] = v
		}
		notes = append(notes, "renamed claw_max_tool_iterations to max_tool_iterations ("+migrationGuide+")")
	}
	if len(notes) == 0 {
		return data, nil, false
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(raw); err != nil {
		return data, nil, false
	}
	return buf.Bytes(), notes, true
}

// isJSONNumber reports whether v decodes as a JSON number, as opposed to a
// string, bool, object, array or null (#144 W6b review, M7).
func isJSONNumber(v json.RawMessage) bool {
	var f float64
	return json.Unmarshal(v, &f) == nil
}

// failedMigrations remembers, per process, every config path whose migrated
// save has already failed once (e.g. a read-only ~/.celeste). Every load
// re-reads the same unmigrated bytes from disk, so without this a path that
// can't be saved would re-warn and retry the write (including the Windows
// rename-retry loop) on every single load — inside a running chat TUI, every
// render that reaches this file (#144 W6b review, I1(c)).
var (
	failedMigrationsMu sync.Mutex
	failedMigrations   = map[string]bool{}
)

// migrateFile runs migrateLegacyKeys on a config file's bytes, prints one
// note per migrated key and saves the result in place with the file's
// current permissions. It returns the bytes to parse: the migrated ones even
// when the save fails (a read-only config still loads, with a warning). A
// path whose save has already failed once is never warned about or retried
// again (see failedMigrations).
func migrateFile(path string, data []byte) []byte {
	out, notes, changed := migrateLegacyKeys(data)
	if !changed {
		return data
	}
	failedMigrationsMu.Lock()
	knownBad := failedMigrations[path]
	failedMigrationsMu.Unlock()
	if knownBad {
		return out
	}
	for _, n := range notes {
		MigrationWarn(n + " in " + path)
	}
	perm := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	// Migration now runs on every load of an old config, not just an
	// explicit save, so a crash mid-write must never leave a truncated,
	// unparseable config behind (review finding, #144 W6b). Write a temp
	// file and rename over path, as permissions.json's save already does.
	if err := writeMigratedConfigAtomic(path, out, perm); err != nil {
		MigrationWarn(fmt.Sprintf("could not save the migrated config %s: %v", path, err))
		failedMigrationsMu.Lock()
		failedMigrations[path] = true
		failedMigrationsMu.Unlock()
	}
	return out
}

// MigrateConfigDir migrates every config*.json profile in the config
// directory once, up front. Call it from the chat startup path before the
// Bubble Tea alt screen opens (main.go's runChatTUI): by the time a running
// chat switches profiles (/endpoint, SwitchEndpoint), every file is already
// migrated, so migrateFile is a no-op instead of printing notes into the alt
// screen on every switch (#144 W6b review, I1(a)). Notes still go to
// MigrationWarn, which is stderr at this point — the alt screen isn't open
// yet. Load and LoadNamed still call migrateFile defensively; this just
// does the work for every profile before either of them needs to.
func MigrateConfigDir() {
	names, err := ListConfigs()
	if err != nil {
		return
	}
	for _, name := range names {
		path := NamedConfigPath(name)
		if name == "default" {
			path = NamedConfigPath("")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		migrateFile(path, data)
	}
}

// writeMigratedConfigAtomic replaces path with data so a reader (another
// celeste process starting mid-migration) sees either the old file or the
// fully migrated one, never a half-written one: a temp file in the same
// directory, synced, then renamed over path with retries (Windows fails a
// rename over a file another process or goroutine has open for reading
// until it closes it — the same race permissions.json's save handles).
//
// path is resolved through any symlinks first (dotfile managers commonly
// symlink ~/.celeste/config.json to a file they track elsewhere): the temp
// file is created next to, and the rename lands on, the real target, so the
// symlink itself is left alone instead of being replaced by a plain file
// that the dotfile manager no longer sees (#144 W6b review, I3). When path
// isn't a symlink, EvalSymlinks returns it unchanged.
func writeMigratedConfigAtomic(path string, data []byte, perm os.FileMode) (err error) {
	target := path
	if resolved, evalErr := filepath.EvalSymlinks(path); evalErr == nil {
		target = resolved
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	// Close before rename: Windows cannot rename an open file.
	if err = tmp.Close(); err != nil {
		return err
	}
	for i := 0; i < 20; i++ {
		if err = os.Rename(tmpName, target); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}
