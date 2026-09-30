package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
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
	if err := os.WriteFile(path, out, perm); err != nil {
		MigrationWarn(fmt.Sprintf("could not save the migrated config %s: %v", path, err))
	}
	return out
}
