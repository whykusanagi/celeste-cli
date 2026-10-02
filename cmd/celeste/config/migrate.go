package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/atomicfile"
)

// Config keys celeste 2.0 removed or renamed (#144, spec §6.2).
const (
	legacyRuntimeModeKey = "runtime_mode"
	legacyClawMaxIterKey = "claw_max_tool_iterations"
	maxToolIterationsKey = "max_tool_iterations"
	legacySkipPersonaKey = "skip_persona_prompt"
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
//     the user actually wrote, not the renamed one — #144 W6b review, M7);
//   - skip_persona_prompt: true is dropped (the persona is always on in
//     chat and agent runs, W5); false is left in place, since it changes
//     nothing and json ignores the unknown key.
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
	if v, ok := raw[typingSpeedKey]; ok && isOldTypingSpeedDefault(v) {
		delete(raw, typingSpeedKey)
		notes = append(notes, fmt.Sprintf("removed typing_speed: it is honoured now, and this value was an old default that would type slower than before; the new default is %d chars/sec (%s)", DefaultTypingSpeed, migrationGuide))
	}
	if v, ok := raw[legacySkipPersonaKey]; ok && bytes.Equal(bytes.TrimSpace(v), []byte("true")) {
		delete(raw, legacySkipPersonaKey)
		notes = append(notes, "removed skip_persona_prompt: the persona is always on in chat and agent runs ("+migrationGuide+")")
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

const typingSpeedKey = "typing_speed"

// isOldTypingSpeedDefault reports whether v is a typing_speed celeste itself
// once wrote as a default (40: DefaultConfig; 25: older defaults and --init
// templates), as opposed to one the user chose.
func isOldTypingSpeedDefault(v json.RawMessage) bool {
	var f float64
	if json.Unmarshal(v, &f) != nil {
		return false
	}
	return f == 40 || f == 25
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
	// Migration now runs on every load of an old config, not just an
	// explicit save, so a crash mid-write must never leave a truncated,
	// unparseable config behind (review finding, #144 W6b). The write keeps
	// the file's mode and lands on a symlink's target, not the link (I3).
	if err := atomicfile.WriteKeepMode(path, out, 0o600); err != nil {
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
