package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
//     already sets max_tool_iterations, which wins.
//
// Every other key keeps its value bytes exactly (json.RawMessage, HTML
// escaping off). Keys come out sorted with two-space indentation, as
// celeste's own saves write them. changed is false, and out is data, when no
// legacy key is present or data is not a JSON object.
func migrateLegacyKeys(data []byte) (out []byte, notes []string, changed bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return data, nil, false
	}
	if _, ok := raw[legacyRuntimeModeKey]; ok {
		delete(raw, legacyRuntimeModeKey)
		notes = append(notes, "removed runtime_mode: 2.0 has one chat mode, which always runs tools in a loop ("+migrationGuide+")")
	}
	if v, ok := raw[legacyClawMaxIterKey]; ok {
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

// migrateFile runs migrateLegacyKeys on a config file's bytes, prints one
// note per migrated key and saves the result in place with the file's
// current permissions. It returns the bytes to parse: the migrated ones even
// when the save fails (a read-only config still loads, with a warning).
func migrateFile(path string, data []byte) []byte {
	out, notes, changed := migrateLegacyKeys(data)
	if !changed {
		return data
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
	}
	return out
}

// writeMigratedConfigAtomic replaces path with data so a reader (another
// celeste process starting mid-migration) sees either the old file or the
// fully migrated one, never a half-written one: a temp file in the same
// directory, synced, then renamed over path with retries (Windows fails a
// rename over a file another process or goroutine has open for reading
// until it closes it — the same race permissions.json's save handles).
func writeMigratedConfigAtomic(path string, data []byte, perm os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
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
		if err = os.Rename(tmpName, path); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}
